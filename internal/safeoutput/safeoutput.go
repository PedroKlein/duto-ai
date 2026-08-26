package safeoutput

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"unicode/utf8"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

const (
	None              = "none"
	ConversationReply = "conversation-reply"
	BranchPR          = "branch-pr"

	KindReply   = "conversation.reply"
	KindBranch  = "git.branch.publish"
	KindDraftPR = "pull_request.create_draft"
)

var (
	ErrInvalidPolicy  = errors.New("invalid safe-output policy")
	ErrInvalidRequest = errors.New("invalid safe-output request")
	ErrIncompleteSet  = errors.New("incomplete safe-output operation set")
)

type Repository struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type Origin struct {
	Kind   string `json:"kind"`
	Number int    `json:"number"`
}

type Base struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type Source struct {
	Commit string
	Tree   string
	Bundle []byte
}

type SourceFunc func(context.Context) (Source, error)

type Policy struct {
	OperationSet    string
	PlanSHA256      string
	PolicySHA256    string
	ControlSHA256   string
	ControlJSON     []byte
	CorrelationKey  string
	Repository      Repository
	Origin          Origin
	Base            Base
	BranchPrefix    string
	MaxReplyBytes   int
	MaxPRTitleBytes int
	MaxPRBodyBytes  int
	MaxBundleBytes  int
	Limits          map[string]dtool.ToolLimit
	Source          SourceFunc
}

type ReplyArgs struct {
	Body string `json:"body"`
}

type BranchArgs struct{}

type DraftPRArgs struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type Result struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
}

type Envelope struct {
	Version        int             `json:"version"`
	RequestID      string          `json:"request_id"`
	CorrelationKey string          `json:"correlation_key"`
	Kind           string          `json:"kind"`
	Mode           string          `json:"mode"`
	RunID          string          `json:"run_id"`
	PlanSHA256     string          `json:"plan_sha256"`
	PolicySHA256   string          `json:"policy_sha256"`
	ControlSHA256  string          `json:"control_sha256"`
	Repository     Repository      `json:"repository"`
	Origin         Origin          `json:"origin"`
	Base           Base            `json:"base"`
	SourceCommit   string          `json:"source_commit"`
	DependsOn      []string        `json:"depends_on"`
	Preconditions  json.RawMessage `json:"preconditions"`
	Payload        json.RawMessage `json:"payload"`
}

type Snapshot struct {
	OperationSet     string
	ControlJSON      []byte
	Operations       []Envelope
	SourceBundle     []byte
	RecoveryMetadata []byte
	RecoveryPatch    []byte
}

type Collector struct {
	mu               sync.Mutex
	policy           Policy
	operations       []Envelope
	source           Source
	recoveryMetadata []byte
	recoveryPatch    []byte
}

func New(policy Policy) (*Collector, error) {
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}

	policy.ControlJSON = slices.Clone(policy.ControlJSON)
	policy.Limits = cloneLimits(policy.Limits)

	return &Collector{policy: policy}, nil
}

func RegisterAll(registry *dtool.Registry, collector *Collector) error {
	if registry == nil || collector == nil {
		return ErrInvalidPolicy
	}

	definitions := []struct {
		name string
		new  func() (tool.Tool, error)
	}{
		{name: "safe-output.conversation-reply", new: collector.newReplyTool},
		{name: "safe-output.branch", new: collector.newBranchTool},
		{name: "safe-output.draft-pr", new: collector.newDraftPRTool},
	}
	for _, definition := range definitions {
		if _, selected := collector.policy.Limits[definition.name]; !selected {
			continue
		}

		value, err := definition.new()
		if err != nil {
			return fmt.Errorf("creating tool %s: %w", definition.name, err)
		}

		registry.Register(definition.name, value)
	}

	return nil
}

func (c *Collector) Reply(runID string, args ReplyArgs) (*Result, error) {
	payload, err := marshalPayload(args)
	if err != nil || c.policy.OperationSet != ConversationReply || invalidText(args.Body, c.policy.MaxReplyBytes) {
		return nil, ErrInvalidRequest
	}

	preconditions := mustJSON(struct {
		SubjectState string `json:"subject_state"`
	}{SubjectState: "open"})

	return c.stage(runID, KindReply, c.policy.Base.SHA, nil, preconditions, payload, nil)
}

func (c *Collector) Branch(ctx context.Context, runID string, _ BranchArgs) (*Result, error) {
	if c.policy.OperationSet != BranchPR || c.policy.Source == nil {
		return nil, ErrInvalidRequest
	}

	source, err := c.policy.Source(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading authored publication source: %w", err)
	}

	if source.Commit == "" || source.Tree == "" || source.Commit == c.policy.Base.SHA || len(source.Bundle) == 0 {
		return nil, ErrInvalidRequest
	}

	preconditions := mustJSON(struct {
		TargetRef   string `json:"target_ref"`
		TargetState string `json:"target_state"`
	}{TargetRef: "refs/heads/" + c.policy.BranchPrefix + c.policy.CorrelationKey, TargetState: "absent"})

	result, err := c.stage(runID, KindBranch, source.Commit, nil, preconditions, json.RawMessage("{}"), &source)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Collector) DraftPR(runID string, args DraftPRArgs) (*Result, error) {
	payload, err := marshalPayload(args)
	if err != nil || c.policy.OperationSet != BranchPR || invalidText(args.Title, c.policy.MaxPRTitleBytes) || invalidText(args.Body, c.policy.MaxPRBodyBytes) {
		return nil, ErrInvalidRequest
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.operations) != 1 || c.operations[0].Kind != KindBranch || c.source.Commit == "" {
		return nil, ErrInvalidRequest
	}

	preconditions := mustJSON(struct {
		HeadRef          string `json:"head_ref"`
		PullRequestState string `json:"pull_request_state"`
		Draft            bool   `json:"draft"`
	}{HeadRef: "refs/heads/" + c.policy.BranchPrefix + c.policy.CorrelationKey, PullRequestState: "absent", Draft: true})
	requestID := requestID(runID, KindDraftPR, payload)
	c.operations = append(c.operations, c.envelope(runID, requestID, KindDraftPR, c.source.Commit, []string{c.operations[0].RequestID}, preconditions, payload))

	return &Result{Status: "staged", RequestID: requestID}, nil
}

func (c *Collector) SetRecovery(metadata, patch []byte) error {
	if len(metadata) == 0 || len(patch) == 0 {
		return ErrInvalidRequest
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.recoveryMetadata) != 0 || len(c.recoveryPatch) != 0 {
		return ErrInvalidRequest
	}

	c.recoveryMetadata = slices.Clone(metadata)
	c.recoveryPatch = slices.Clone(patch)

	return nil
}

func (c *Collector) Snapshot(runID string, succeeded bool) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !succeeded {
		return Snapshot{
			OperationSet: None, ControlJSON: slices.Clone(c.policy.ControlJSON),
			RecoveryMetadata: slices.Clone(c.recoveryMetadata), RecoveryPatch: slices.Clone(c.recoveryPatch),
		}, nil
	}

	if err := c.complete(runID); err != nil {
		return Snapshot{}, err
	}

	operations := make([]Envelope, len(c.operations))
	copy(operations, c.operations)

	for index := range operations {
		operations[index].DependsOn = slices.Clone(operations[index].DependsOn)
		operations[index].Payload = slices.Clone(operations[index].Payload)
		operations[index].Preconditions = slices.Clone(operations[index].Preconditions)
	}

	return Snapshot{
		OperationSet: c.policy.OperationSet,
		ControlJSON:  slices.Clone(c.policy.ControlJSON),
		Operations:   operations,
		SourceBundle: slices.Clone(c.source.Bundle),
	}, nil
}

func (c *Collector) stage(runID, kind, sourceCommit string, dependsOn []string, preconditions, payload json.RawMessage, source *Source) (*Result, error) {
	if runID == "" {
		return nil, ErrInvalidRequest
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.operations) != 0 {
		return nil, ErrInvalidRequest
	}

	requestID := requestID(runID, kind, payload)

	c.operations = append(c.operations, c.envelope(runID, requestID, kind, sourceCommit, dependsOn, preconditions, payload))
	if source != nil {
		c.source = Source{Commit: source.Commit, Tree: source.Tree, Bundle: slices.Clone(source.Bundle)}
	}

	return &Result{Status: "staged", RequestID: requestID}, nil
}

func (c *Collector) envelope(runID, requestID, kind, sourceCommit string, dependsOn []string, preconditions, payload json.RawMessage) Envelope {
	return Envelope{
		Version: 1, RequestID: requestID, CorrelationKey: c.policy.CorrelationKey, Kind: kind, Mode: "staged",
		RunID: runID, PlanSHA256: c.policy.PlanSHA256, PolicySHA256: c.policy.PolicySHA256,
		ControlSHA256: c.policy.ControlSHA256, Repository: c.policy.Repository, Origin: c.policy.Origin,
		Base: c.policy.Base, SourceCommit: sourceCommit, DependsOn: slices.Clone(dependsOn),
		Preconditions: slices.Clone(preconditions), Payload: slices.Clone(payload),
	}
}

func (c *Collector) complete(runID string) error {
	switch c.policy.OperationSet {
	case None:
		if len(c.operations) != 0 {
			return ErrIncompleteSet
		}
	case ConversationReply:
		if len(c.operations) != 1 || c.operations[0].Kind != KindReply || c.operations[0].RunID != runID {
			return ErrIncompleteSet
		}
	case BranchPR:
		if len(c.operations) != 2 || c.operations[0].Kind != KindBranch || c.operations[1].Kind != KindDraftPR || c.operations[0].RunID != runID || c.operations[1].RunID != runID {
			return ErrIncompleteSet
		}
	default:
		return ErrIncompleteSet
	}

	return nil
}

func (c *Collector) newReplyTool() (tool.Tool, error) {
	return functiontool.New[ReplyArgs, *Result](functiontool.Config{Name: "safe-output.conversation-reply", Description: "Stage one reply for the runtime-bound conversation."}, func(ctx agent.Context, args ReplyArgs) (*Result, error) {
		return c.Reply(ctx.SessionID(), args)
	})
}

func (c *Collector) newBranchTool() (tool.Tool, error) {
	return functiontool.New[BranchArgs, *Result](functiontool.Config{Name: "safe-output.branch", Description: "Stage publication of the one authored commit to the runtime-bound new branch."}, func(ctx agent.Context, args BranchArgs) (*Result, error) {
		return c.Branch(ctx, ctx.SessionID(), args)
	})
}

func (c *Collector) newDraftPRTool() (tool.Tool, error) {
	return functiontool.New[DraftPRArgs, *Result](functiontool.Config{Name: "safe-output.draft-pr", Description: "Stage one draft pull request for the staged branch."}, func(ctx agent.Context, args DraftPRArgs) (*Result, error) {
		return c.DraftPR(ctx.SessionID(), args)
	})
}

func validatePolicy(policy Policy) error {
	if err := validatePolicyFields(policy); err != nil {
		return err
	}

	switch policy.OperationSet {
	case None:
		if len(policy.Limits) != 0 || policy.Source != nil {
			return ErrInvalidPolicy
		}
	case ConversationReply:
		if policy.Origin.Kind != "issue" && policy.Origin.Kind != "pull_request" || !exactLimits(policy.Limits, "safe-output.conversation-reply") {
			return ErrInvalidPolicy
		}
	case BranchPR:
		if policy.Source == nil || !exactLimits(policy.Limits, "safe-output.branch", "safe-output.draft-pr") {
			return ErrInvalidPolicy
		}
	default:
		return ErrInvalidPolicy
	}

	return nil
}

func validatePolicyFields(policy Policy) error {
	if slices.Contains([]string{policy.CorrelationKey, policy.Repository.ID, policy.Repository.Owner, policy.Repository.Name, policy.Base.Ref}, "") {
		return ErrInvalidPolicy
	}

	for _, value := range []int{policy.MaxReplyBytes, policy.MaxPRTitleBytes, policy.MaxPRBodyBytes, policy.MaxBundleBytes} {
		if value <= 0 {
			return ErrInvalidPolicy
		}
	}

	if !validDigest(policy.PlanSHA256) || !validDigest(policy.PolicySHA256) || !validDigest(policy.ControlSHA256) || len(policy.ControlJSON) == 0 || len(policy.Base.SHA) != 40 || policy.BranchPrefix != "duto/m3/" {
		return ErrInvalidPolicy
	}

	return nil
}

func exactLimits(limits map[string]dtool.ToolLimit, names ...string) bool {
	if len(limits) != len(names) {
		return false
	}

	for _, name := range names {
		limit, exists := limits[name]
		if !exists || limit.MaxCalls != 1 || limit.Timeout <= 0 || limit.MaxRequestBytes <= 0 || limit.MaxResultBytes <= 0 {
			return false
		}
	}

	return true
}

func cloneLimits(source map[string]dtool.ToolLimit) map[string]dtool.ToolLimit {
	result := make(map[string]dtool.ToolLimit, len(source))
	maps.Copy(result, source)

	return result
}

func invalidText(value string, maximum int) bool {
	return value == "" || len(value) > maximum || !utf8.ValidString(value)
}

func marshalPayload(value any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding safe-output payload: %w", err)
	}

	return encoded, nil
}

func requestID(runID, kind string, payload []byte) string {
	payloadSum := sha256.Sum256(payload)
	hash := sha256.New()
	hash.Write([]byte(runID))
	hash.Write([]byte{0})
	hash.Write([]byte(kind))
	hash.Write([]byte{0})
	hash.Write([]byte(hex.EncodeToString(payloadSum[:])))

	return hex.EncodeToString(hash.Sum(nil))
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}

	_, err := hex.DecodeString(value)

	return err == nil
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return encoded
}
