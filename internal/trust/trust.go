package trust

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxControlEvidenceBytes = 65_536
	transportNone           = "none"
	transportStaged         = "staged"
	subjectNone             = "none"
)

var (
	errEvidenceStdin       = errors.New("control-evidence stdin is invalid")
	errEvidenceNotRegular  = errors.New("control-evidence file must be a regular non-symlink file")
	errEvidenceTooLarge    = errors.New("control-evidence file exceeds 65536 bytes")
	errEvidenceChanged     = errors.New("control-evidence file changed while opening")
	errEvidenceInvalidUTF8 = errors.New("control-evidence file is not valid UTF-8")
	errTrailingJSON        = errors.New("trailing JSON value")
	errJSONNull            = errors.New("explicit null is invalid")
	errJSONKey             = errors.New("object key is not a string")
	errJSONDuplicate       = errors.New("duplicate field")
	errJSONDelimiter       = errors.New("invalid JSON delimiter")
	errJSONClosing         = errors.New("invalid JSON closing delimiter")
)

type Context string

const (
	ContextForkedPR       Context = "forked_pr"
	ContextSameRepository Context = "same_repository"
	ContextScheduled      Context = "scheduled"
	ContextLocal          Context = "local"
	ContextUnknown        Context = "unknown"
)

type Capability string

const (
	CapabilityWorkspaceRead   Capability = "workspace.read"
	CapabilityWorkspaceMutate Capability = "workspace.mutate"
	CapabilityGitRead         Capability = "git.read"
	CapabilityGitMutate       Capability = "git.mutate"
	CapabilityGitPublish      Capability = "git.publish"
	CapabilityProcessExec     Capability = "process.exec"
	CapabilityNetworkRead     Capability = "network.read"
	CapabilityNetworkMutate   Capability = "network.mutate"
	CapabilityGitHubRead      Capability = "github.read"
	CapabilityGitHubMutate    Capability = "github.mutate"
	CapabilityAgentCall       Capability = "agent.call"
)

type Eligibility string

const (
	EligibilityReadOnly Eligibility = "read"
	EligibilityGrant    Eligibility = "grant"
	EligibilityStaged   Eligibility = "staged"
	EligibilityDenied   Eligibility = "denied"
)

type Decision struct {
	Context       Context
	AdmissionID   string
	ControlSHA256 string
	Transport     string
	CheckoutRef   string
	CheckoutSHA   string
	Present       bool
}

type controlEvidence struct {
	Version    int        `json:"version"`
	Source     string     `json:"source"`
	Repository repository `json:"repository"`
	Event      *event     `json:"event,omitempty"`
	Run        *run       `json:"run,omitempty"`
	Operator   *operator  `json:"operator,omitempty"`
	Checkout   checkout   `json:"checkout"`
	Admission  admission  `json:"admission"`
}

type repository struct {
	ID            string `json:"id"`
	OwnerID       string `json:"owner_id"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

type event struct {
	Name    string  `json:"name"`
	ActorID string  `json:"actor_id"`
	Subject subject `json:"subject"`
	Base    ref     `json:"base"`
	Head    ref     `json:"head"`
}

type subject struct {
	Kind   string `json:"kind"`
	Number int    `json:"number"`
}

type ref struct {
	RepositoryID string `json:"repository_id"`
	Ref          string `json:"ref"`
	SHA          string `json:"sha"`
}

type run struct {
	ID          string `json:"id"`
	Attempt     int    `json:"attempt"`
	WorkflowSHA string `json:"workflow_sha"`
}

type operator struct {
	Profile string `json:"profile"`
}

type checkout struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type admission struct {
	ID             string `json:"id"`
	CorrelationKey string `json:"correlation_key"`
	IssuedAt       string `json:"issued_at"`
	ExpiresAt      string `json:"expires_at"`
}

var (
	decimalPattern   = regexp.MustCompile(`^\d+$`)
	resourcePattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	localIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,126}$`)
	namePattern      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	admissionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	shaPattern       = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func Unknown() Decision {
	return Decision{Context: ContextUnknown, Transport: transportNone}
}

func Load(path string, now time.Time) (Decision, error) {
	if path == "" {
		return Unknown(), nil
	}

	if path == "-" {
		return Decision{}, errEvidenceStdin
	}

	info, err := os.Lstat(path)
	if err != nil {
		return Decision{}, fmt.Errorf("checking control-evidence file: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Decision{}, errEvidenceNotRegular
	}

	if info.Size() > MaxControlEvidenceBytes {
		return Decision{}, errEvidenceTooLarge
	}

	file, err := os.Open(path) //nolint:gosec // caller explicitly selects trusted control evidence
	if err != nil {
		return Decision{}, fmt.Errorf("opening control-evidence file: %w", err)
	}

	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return Decision{}, fmt.Errorf("checking opened control-evidence file: %w", err)
	}

	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()

		return Decision{}, errEvidenceChanged
	}

	data, readErr := io.ReadAll(io.LimitReader(file, MaxControlEvidenceBytes+1))
	closeErr := file.Close()

	if readErr != nil {
		return Decision{}, fmt.Errorf("reading control-evidence file: %w", readErr)
	}

	if closeErr != nil {
		return Decision{}, fmt.Errorf("closing control-evidence file: %w", closeErr)
	}

	if len(data) > MaxControlEvidenceBytes {
		return Decision{}, errEvidenceTooLarge
	}

	return Decode(data, now)
}

func Decode(data []byte, now time.Time) (Decision, error) {
	if !utf8.Valid(data) {
		return Decision{}, errEvidenceInvalidUTF8
	}

	if err := validateJSON(data); err != nil {
		return Decision{}, fmt.Errorf("decoding control-evidence JSON: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var evidence controlEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return Decision{}, fmt.Errorf("decoding control-evidence JSON: %w", err)
	}

	sum := sha256.Sum256(data)

	decision := Decision{
		Context:       ContextUnknown,
		AdmissionID:   evidence.Admission.ID,
		ControlSHA256: hex.EncodeToString(sum[:]),
		Transport:     transportStaged,
		CheckoutRef:   evidence.Checkout.Ref,
		CheckoutSHA:   evidence.Checkout.SHA,
		Present:       true,
	}
	if !validCommon(evidence, now.UTC()) {
		return decision, nil
	}

	switch evidence.Source {
	case "local":
		if validLocal(evidence) {
			decision.Context = ContextLocal
		}
	case "github":
		decision.Context = githubContext(evidence)
	}

	return decision, nil
}

func validCommon(value controlEvidence, now time.Time) bool {
	if value.Version != 1 || !admissionPattern.MatchString(value.Admission.ID) || !admissionPattern.MatchString(value.Admission.CorrelationKey) ||
		!validCheckout(value.Checkout) || !resourcePattern.MatchString(value.Repository.Owner) || !resourcePattern.MatchString(value.Repository.Name) ||
		!namePattern.MatchString(value.Repository.DefaultBranch) {
		return false
	}

	issued, err := time.Parse(time.RFC3339, value.Admission.IssuedAt)
	if err != nil || issued.UTC().Format(time.RFC3339) != value.Admission.IssuedAt {
		return false
	}

	expires, err := time.Parse(time.RFC3339, value.Admission.ExpiresAt)
	if err != nil || expires.UTC().Format(time.RFC3339) != value.Admission.ExpiresAt || !expires.After(issued) || expires.Sub(issued) > 6*time.Hour {
		return false
	}

	return !now.Before(issued) && now.Before(expires)
}

func validLocal(value controlEvidence) bool {
	return value.Event == nil && value.Run == nil && value.Operator != nil && namePattern.MatchString(value.Operator.Profile) &&
		localIDPattern.MatchString(value.Repository.ID) && localIDPattern.MatchString(value.Repository.OwnerID)
}

func githubContext(value controlEvidence) Context {
	if !validGitHub(value) {
		return ContextUnknown
	}

	switch value.Event.Name {
	case "pull_request":
		return pullRequestContext(value)
	case "schedule":
		return scheduledContext(value)
	case "workflow_dispatch":
		return sameRepositoryContext(value)
	case "push":
		if value.Event.Head.RepositoryID == value.Repository.ID {
			return ContextSameRepository
		}
	case "issues", "issue_comment":
		if sameCheckout(value) {
			return ContextSameRepository
		}
	}

	return ContextUnknown
}

func validGitHub(value controlEvidence) bool {
	return value.Event != nil && value.Run != nil && value.Operator == nil && decimalPattern.MatchString(value.Repository.ID) &&
		decimalPattern.MatchString(value.Repository.OwnerID) && decimalPattern.MatchString(value.Event.ActorID) &&
		decimalPattern.MatchString(value.Run.ID) && value.Run.Attempt > 0 && shaPattern.MatchString(value.Run.WorkflowSHA) &&
		validRef(value.Event.Base) && validRef(value.Event.Head) && value.Event.Base.RepositoryID == value.Repository.ID &&
		value.Checkout.Ref == value.Event.Base.Ref && value.Checkout.SHA == value.Event.Base.SHA && validSubject(value.Event.Subject)
}

func pullRequestContext(value controlEvidence) Context {
	if value.Event.Subject.Kind != "pull_request" {
		return ContextUnknown
	}

	if value.Event.Head.RepositoryID != value.Repository.ID {
		return ContextForkedPR
	}

	return ContextSameRepository
}

func scheduledContext(value controlEvidence) Context {
	if !sameCheckout(value) || value.Event.Subject.Kind != subjectNone {
		return ContextUnknown
	}

	return ContextScheduled
}

func sameRepositoryContext(value controlEvidence) Context {
	if !sameCheckout(value) || value.Event.Subject.Kind != subjectNone {
		return ContextUnknown
	}

	return ContextSameRepository
}

func sameCheckout(value controlEvidence) bool {
	return value.Event.Base.RepositoryID == value.Repository.ID && value.Event.Head.RepositoryID == value.Repository.ID &&
		value.Event.Base.Ref == value.Checkout.Ref && value.Event.Head.Ref == value.Checkout.Ref &&
		value.Event.Base.SHA == value.Checkout.SHA && value.Event.Head.SHA == value.Checkout.SHA
}

func validCheckout(value checkout) bool {
	return strings.HasPrefix(value.Ref, "refs/heads/") && len(value.Ref) > len("refs/heads/") && shaPattern.MatchString(value.SHA)
}

func validRef(value ref) bool {
	return decimalPattern.MatchString(value.RepositoryID) && strings.HasPrefix(value.Ref, "refs/") && shaPattern.MatchString(value.SHA)
}

func validSubject(value subject) bool {
	switch value.Kind {
	case subjectNone:
		return value.Number == 0
	case "issue", "pull_request":
		return value.Number > 0
	default:
		return false
	}
}

func EligibilityFor(context Context, capability Capability) Eligibility {
	switch capability {
	case CapabilityWorkspaceRead, CapabilityGitRead, CapabilityNetworkRead, CapabilityGitHubRead:
		return EligibilityReadOnly
	case CapabilityWorkspaceMutate, CapabilityGitMutate, CapabilityProcessExec:
		if context == ContextSameRepository || context == ContextScheduled || context == ContextLocal {
			return EligibilityGrant
		}

		return EligibilityDenied
	case CapabilityGitPublish, CapabilityGitHubMutate:
		return EligibilityStaged
	case CapabilityNetworkMutate:
		return EligibilityDenied
	case CapabilityAgentCall:
		if context == ContextForkedPR || context == ContextUnknown {
			return EligibilityReadOnly
		}

		return EligibilityGrant
	default:
		return EligibilityDenied
	}
}

func validateJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	if validationErr := validateJSONValue(decoder); validationErr != nil {
		return validationErr
	}

	if _, tokenErr := decoder.Token(); !errors.Is(tokenErr, io.EOF) {
		if tokenErr == nil {
			return errTrailingJSON
		}

		return fmt.Errorf("reading trailing JSON token: %w", tokenErr)
	}

	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("reading JSON token: %w", err)
	}

	if token == nil {
		return errJSONNull
	}

	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		seen := map[string]struct{}{}

		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("reading JSON object key: %w", keyErr)
			}

			key, ok := keyToken.(string)
			if !ok {
				return errJSONKey
			}

			if _, exists := seen[key]; exists {
				return errJSONDuplicate
			}

			seen[key] = struct{}{}

			if valueErr := validateJSONValue(decoder); valueErr != nil {
				return valueErr
			}
		}
	case '[':
		for decoder.More() {
			if valueErr := validateJSONValue(decoder); valueErr != nil {
				return valueErr
			}
		}
	default:
		return errJSONDelimiter
	}

	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("reading JSON closing delimiter: %w", err)
	}

	if closing != map[json.Delim]json.Delim{'{': '}', '[': ']'}[delim] {
		return errJSONClosing
	}

	return nil
}
