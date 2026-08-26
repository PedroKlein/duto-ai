package publisher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/safeoutput"
	"github.com/PedroKlein/duto-ai/internal/trust"
)

const (
	maxManifestBytes = 131_072
	maxBundleFiles   = 12
)

type VerifyInput struct {
	Config               *config.Config
	ControlEvidencePath  string
	BundlePath           string
	ExpectedBundleSHA256 string
	PermissionProfile    string
	Now                  time.Time
}

type Verified struct {
	bundleSHA256 string
	planSHA256   string
	policySHA256 string
	repositoryID string
	operationSet string
	operations   []Operation
}

type bundleManifest struct {
	Version       int          `json:"version"`
	BundleKind    string       `json:"bundle_kind"`
	RunID         string       `json:"run_id"`
	Completion    string       `json:"completion"`
	OperationSet  string       `json:"operation_set"`
	PlanSHA256    string       `json:"plan_sha256"`
	PolicySHA256  string       `json:"policy_sha256"`
	ControlSHA256 string       `json:"control_sha256"`
	RepositoryID  string       `json:"repository_id"`
	BaseRef       string       `json:"base_ref"`
	BaseSHA       string       `json:"base_sha"`
	SourceCommit  string       `json:"source_commit"`
	Files         []bundleFile `json:"files"`
}

type bundleFile struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type operationEnvelope struct {
	Version        int             `json:"version"`
	RequestID      string          `json:"request_id"`
	CorrelationKey string          `json:"correlation_key"`
	Kind           string          `json:"kind"`
	Mode           string          `json:"mode"`
	RunID          string          `json:"run_id"`
	PlanSHA256     string          `json:"plan_sha256"`
	PolicySHA256   string          `json:"policy_sha256"`
	ControlSHA256  string          `json:"control_sha256"`
	Repository     repository      `json:"repository"`
	Origin         origin          `json:"origin"`
	Base           base            `json:"base"`
	SourceCommit   string          `json:"source_commit"`
	DependsOn      []string        `json:"depends_on"`
	Preconditions  json.RawMessage `json:"preconditions"`
	Payload        json.RawMessage `json:"payload"`
}

type repository struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type origin struct {
	Kind   string `json:"kind"`
	Number int    `json:"number"`
}

type base struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type controlIdentity struct {
	Version    int    `json:"version"`
	Source     string `json:"source"`
	Repository struct {
		ID            string `json:"id"`
		OwnerID       string `json:"owner_id"`
		Owner         string `json:"owner"`
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
	Event *struct {
		Name    string `json:"name"`
		ActorID string `json:"actor_id"`
		Subject origin `json:"subject"`
		Base    struct {
			RepositoryID string `json:"repository_id"`
			Ref          string `json:"ref"`
			SHA          string `json:"sha"`
		} `json:"base"`
		Head struct {
			RepositoryID string `json:"repository_id"`
			Ref          string `json:"ref"`
			SHA          string `json:"sha"`
		} `json:"head"`
	} `json:"event,omitempty"`
	Run *struct {
		ID          string `json:"id"`
		Attempt     int    `json:"attempt"`
		WorkflowSHA string `json:"workflow_sha"`
	} `json:"run,omitempty"`
	Operator *struct {
		Profile string `json:"profile"`
	} `json:"operator,omitempty"`
	Checkout  base `json:"checkout"`
	Admission struct {
		ID             string `json:"id"`
		CorrelationKey string `json:"correlation_key"`
		IssuedAt       string `json:"issued_at"`
		ExpiresAt      string `json:"expires_at"`
	} `json:"admission"`
}

func Verify(input VerifyInput) (*Verified, error) {
	if err := validateVerifyInput(input); err != nil {
		return nil, err
	}

	root, manifestBytes, files, err := readBundle(input.BundlePath, input.Config.M3.Publication.MaxBundleBytes)
	if err != nil {
		return nil, err
	}
	defer root.Close() //nolint:errcheck // verification result is authoritative

	manifestDigest := digest(manifestBytes)
	if manifestDigest != input.ExpectedBundleSHA256 {
		return nil, fmt.Errorf("bundle sha256 mismatch: %w", ErrRejected)
	}

	var manifest bundleManifest
	if decodeErr := decodeStrict(manifestBytes, &manifest); decodeErr != nil {
		return nil, fmt.Errorf("decoding manifest: %w", ErrRejected)
	}

	policyDigest, err := plan.M3PolicySHA256(input.Config)
	if err != nil || !validManifest(manifest, policyDigest, input.PermissionProfile) {
		return nil, fmt.Errorf("validating manifest policy: %w", ErrRejected)
	}

	if filesErr := verifyManifestFiles(root, manifest, manifestBytes, files, input.Config.M3.Publication.MaxBundleBytes); filesErr != nil {
		return nil, filesErr
	}

	bundledDecision, err := verifyControlEvidence(root, manifest, input)
	if err != nil {
		return nil, err
	}

	workspace := input.Config.Workspaces[input.Config.M3.Authoring.Workspace]

	operations, err := verifyOperations(root, manifest, bundledDecision, input.BundlePath, workspace.Root, input.Config.M3.Publication)
	if err != nil {
		return nil, err
	}

	return &Verified{
		bundleSHA256: manifestDigest,
		planSHA256:   manifest.PlanSHA256,
		policySHA256: manifest.PolicySHA256,
		repositoryID: manifest.RepositoryID,
		operationSet: manifest.OperationSet,
		operations:   operations,
	}, nil
}

func verifyControlEvidence(root *os.Root, manifest bundleManifest, input VerifyInput) (trust.Decision, error) {
	bundledControl, err := root.ReadFile("control.json")
	if err != nil || digest(bundledControl) != manifest.ControlSHA256 {
		return trust.Decision{}, fmt.Errorf("reading bundled control evidence: %w", ErrRejected)
	}

	bundledDecision, err := trust.Decode(bundledControl, input.Now)
	if err != nil || !publishableDecision(bundledDecision) {
		return trust.Decision{}, fmt.Errorf("validating bundled control evidence: %w", ErrRejected)
	}

	currentDecision, err := trust.Load(input.ControlEvidencePath, input.Now)
	if err != nil || !publishableDecision(currentDecision) {
		return trust.Decision{}, fmt.Errorf("validating current control evidence: %w", ErrRejected)
	}

	if identityErr := compareControlIdentity(bundledControl, currentDecision.ControlJSON); identityErr != nil {
		return trust.Decision{}, identityErr
	}

	if manifest.RepositoryID != bundledDecision.Repository.ID || manifest.BaseRef != bundledDecision.CheckoutRef || manifest.BaseSHA != bundledDecision.CheckoutSHA || bundledDecision.AdmissionID != input.Config.M3.Admission.ID {
		return trust.Decision{}, fmt.Errorf("control evidence does not match manifest: %w", ErrRejected)
	}

	return bundledDecision, nil
}

func validateVerifyInput(input VerifyInput) error {
	if input.Config == nil || input.Config.M3 == nil || input.ControlEvidencePath == "" || input.ControlEvidencePath == "-" || input.BundlePath == "" || !validDigest(input.ExpectedBundleSHA256) {
		return ErrRejected
	}

	if input.PermissionProfile != "reply" && input.PermissionProfile != safeoutput.BranchPR {
		return ErrRejected
	}

	if input.Now.IsZero() {
		return ErrRejected
	}

	return nil
}

func readBundle(path string, maximum int) (root *os.Root, manifestBytes []byte, names []string, err error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, nil, nil, ErrRejected
	}

	root, err = os.OpenRoot(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("opening bundle root: %w", ErrRejected)
	}

	manifestInfo, err := root.Lstat("manifest.json")
	if err != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Size() <= 0 || manifestInfo.Size() > maxManifestBytes {
		_ = root.Close()
		return nil, nil, nil, ErrRejected
	}

	manifestBytes, err = root.ReadFile("manifest.json")
	if err != nil || !utf8.Valid(manifestBytes) {
		_ = root.Close()
		return nil, nil, nil, ErrRejected
	}

	names, err = bundleFileNames(root, maximum)
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, err
	}

	slices.Sort(names)

	return root, manifestBytes, names, nil
}

func bundleFileNames(root *os.Root, maximum int) ([]string, error) {
	var names []string

	total := 0

	walkErr := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if name == "." || entry.IsDir() {
			return nil
		}

		fileInfo, statErr := root.Lstat(name)
		if statErr != nil || fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
			return ErrRejected
		}

		total += int(fileInfo.Size())
		if total > maximum {
			return ErrRejected
		}

		names = append(names, filepath.ToSlash(name))

		return nil
	})
	if walkErr != nil || len(names) > maxBundleFiles+1 {
		return nil, ErrRejected
	}

	return names, nil
}

func validManifest(manifest bundleManifest, policyDigest, profile string) bool {
	if manifest.Version != 2 || manifest.BundleKind != "m3-authoring" || manifest.Completion != "succeeded" || manifest.RunID == "" || manifest.PolicySHA256 != policyDigest ||
		!validDigest(manifest.PlanSHA256) || !validDigest(manifest.PolicySHA256) || !validDigest(manifest.ControlSHA256) || manifest.RepositoryID == "" || !validRef(manifest.BaseRef) || !validCommit(manifest.BaseSHA) || !validCommit(manifest.SourceCommit) {
		return false
	}

	return operationSetMatchesProfile(manifest.OperationSet, profile)
}

func operationSetMatchesProfile(operationSet, profile string) bool {
	switch operationSet {
	case safeoutput.ConversationReply:
		return profile == "reply"
	case safeoutput.BranchPR:
		return profile == safeoutput.BranchPR
	default:
		return false
	}
}

func verifyManifestFiles(root *os.Root, manifest bundleManifest, manifestBytes []byte, actual []string, maximum int) error {
	if len(manifest.Files) == 0 || len(manifest.Files) > maxBundleFiles {
		return ErrRejected
	}

	expected := make([]string, 0, len(manifest.Files)+1)
	total := len(manifestBytes)
	previous := ""

	for _, entry := range manifest.Files {
		if entry.Name <= previous {
			return ErrRejected
		}

		contents, err := verifyBundleFile(root, entry)
		if err != nil {
			return err
		}

		previous = entry.Name
		total += len(contents)

		expected = append(expected, entry.Name)
	}

	expected = append(expected, "manifest.json")
	slices.Sort(expected)

	if !slices.Equal(expected, actual) || total > maximum {
		return ErrRejected
	}

	return nil
}

func verifyBundleFile(root *os.Root, entry bundleFile) ([]byte, error) {
	if !validBundleName(entry.Name) || entry.Size <= 0 || !validDigest(entry.SHA256) {
		return nil, ErrRejected
	}

	info, err := root.Lstat(entry.Name)
	if err != nil || !info.Mode().IsRegular() || int(info.Size()) != entry.Size {
		return nil, ErrRejected
	}

	contents, err := root.ReadFile(entry.Name)
	if err != nil || digest(contents) != entry.SHA256 {
		return nil, ErrRejected
	}

	if strings.HasSuffix(entry.Name, ".json") && (len(contents) > maxManifestBytes || !utf8.Valid(contents)) {
		return nil, ErrRejected
	}

	return contents, nil
}

func validBundleName(name string) bool {
	if name == "" || name == "manifest.json" || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00") {
		return false
	}

	return true
}

func publishableDecision(decision trust.Decision) bool {
	return decision.Present && decision.Context != trust.ContextUnknown && decision.Context != trust.ContextForkedPR && decision.ControlSHA256 != ""
}

func compareControlIdentity(bundled, current []byte) error {
	var bundledIdentity, currentIdentity controlIdentity
	if decodeStrict(bundled, &bundledIdentity) != nil || decodeStrict(current, &currentIdentity) != nil {
		return ErrRejected
	}

	bundledIdentity.Admission.IssuedAt = ""
	bundledIdentity.Admission.ExpiresAt = ""
	currentIdentity.Admission.IssuedAt = ""
	currentIdentity.Admission.ExpiresAt = ""

	left, _ := json.Marshal(bundledIdentity)

	right, _ := json.Marshal(currentIdentity)
	if !bytes.Equal(left, right) {
		return fmt.Errorf("control identity mismatch: %w", ErrRejected)
	}

	return nil
}

func verifyOperations(root *os.Root, manifest bundleManifest, decision trust.Decision, bundlePath, repositoryRoot string, policy config.M3Publication) ([]Operation, error) {
	names := operationNames(manifest.OperationSet)
	if names == nil {
		return nil, ErrRejected
	}

	operations := make([]Operation, len(names))

	envelopes := make([]operationEnvelope, len(names))
	for index, name := range names {
		data, err := root.ReadFile(name)
		if err != nil || decodeStrict(data, &envelopes[index]) != nil {
			return nil, ErrRejected
		}

		operation, err := validateOperation(envelopes[index], manifest, decision, policy)
		if err != nil {
			return nil, err
		}

		operations[index] = operation
	}

	if manifest.OperationSet == safeoutput.ConversationReply {
		if manifest.SourceCommit != manifest.BaseSHA || len(envelopes[0].DependsOn) != 0 {
			return nil, ErrRejected
		}

		return operations, nil
	}

	if len(envelopes[0].DependsOn) != 0 || !slices.Equal(envelopes[1].DependsOn, []string{envelopes[0].RequestID}) || manifest.SourceCommit == manifest.BaseSHA {
		return nil, ErrRejected
	}

	if err := verifyAuthoredBundle(repositoryRoot, filepath.Join(bundlePath, "authored.bundle"), manifest.BaseSHA, manifest.SourceCommit); err != nil {
		return nil, err
	}

	operations[0].SourceBundle = filepath.Join(bundlePath, "authored.bundle")

	return operations, nil
}

func operationNames(operationSet string) []string {
	switch operationSet {
	case safeoutput.ConversationReply:
		return []string{"operations/0001-conversation-reply.json"}
	case safeoutput.BranchPR:
		return []string{"operations/0001-branch.json", "operations/0002-draft-pr.json"}
	default:
		return nil
	}
}

func validateOperation(envelope operationEnvelope, manifest bundleManifest, decision trust.Decision, policy config.M3Publication) (Operation, error) {
	if !validOperationHeader(envelope, manifest, decision) {
		return Operation{}, ErrRejected
	}

	payloadDigest := digest(envelope.Payload)
	if envelope.RequestID != requestID(envelope.RunID, envelope.Kind, payloadDigest) {
		return Operation{}, ErrRejected
	}

	operation := Operation{
		RequestID: envelope.RequestID, CorrelationKey: envelope.CorrelationKey, Kind: envelope.Kind,
		RepositoryOwner: envelope.Repository.Owner, RepositoryName: envelope.Repository.Name,
		OriginKind: envelope.Origin.Kind, OriginNumber: envelope.Origin.Number, BaseRef: envelope.Base.Ref,
		BaseSHA: envelope.Base.SHA, SourceCommit: envelope.SourceCommit, PayloadSHA256: payloadDigest,
	}

	switch envelope.Kind {
	case safeoutput.KindReply:
		return validateReplyOperation(envelope, decision, policy, operation)
	case safeoutput.KindBranch:
		return validateBranchOperation(envelope, decision, policy, operation)
	case safeoutput.KindDraftPR:
		return validateDraftPROperation(envelope, decision, policy, operation)
	default:
		return Operation{}, ErrRejected
	}
}

func validOperationHeader(envelope operationEnvelope, manifest bundleManifest, decision trust.Decision) bool {
	return envelopeBoundToManifest(envelope, manifest) && envelopeBoundToDecision(envelope, decision)
}

func envelopeBoundToManifest(envelope operationEnvelope, manifest bundleManifest) bool {
	return envelope.Version == 1 && envelope.Mode == "staged" && envelope.RunID == manifest.RunID &&
		envelope.PlanSHA256 == manifest.PlanSHA256 && envelope.PolicySHA256 == manifest.PolicySHA256 && envelope.ControlSHA256 == manifest.ControlSHA256 &&
		envelope.Base.Ref == manifest.BaseRef && envelope.Base.SHA == manifest.BaseSHA && envelope.SourceCommit == manifest.SourceCommit && validDigest(envelope.RequestID)
}

func envelopeBoundToDecision(envelope operationEnvelope, decision trust.Decision) bool {
	return envelope.Repository.ID == decision.Repository.ID && envelope.Repository.Owner == decision.Repository.Owner && envelope.Repository.Name == decision.Repository.Name &&
		envelope.Origin.Kind == decision.Origin.Kind && envelope.Origin.Number == decision.Origin.Number && envelope.CorrelationKey == decision.CorrelationKey
}

func validateReplyOperation(envelope operationEnvelope, decision trust.Decision, policy config.M3Publication, operation Operation) (Operation, error) {
	var (
		preconditions struct {
			SubjectState string `json:"subject_state"`
		}
		payload struct {
			Body string `json:"body"`
		}
	)
	if decodeStrict(envelope.Preconditions, &preconditions) != nil || preconditions.SubjectState != "open" || decodeStrict(envelope.Payload, &payload) != nil || invalidText(payload.Body, policy.MaxReplyBytes) || decision.Origin.Kind == "none" {
		return Operation{}, ErrRejected
	}

	operation.ReplyBody = payload.Body

	return operation, nil
}

func validateBranchOperation(envelope operationEnvelope, decision trust.Decision, policy config.M3Publication, operation Operation) (Operation, error) {
	var (
		preconditions struct {
			TargetRef   string `json:"target_ref"`
			TargetState string `json:"target_state"`
		}
		payload struct{}
	)

	expected := "refs/heads/" + policy.BranchPrefix + decision.CorrelationKey
	if decodeStrict(envelope.Preconditions, &preconditions) != nil || preconditions.TargetState != "absent" || preconditions.TargetRef != expected || preconditions.TargetRef == envelope.Base.Ref || decodeStrict(envelope.Payload, &payload) != nil {
		return Operation{}, ErrRejected
	}

	operation.TargetRef = preconditions.TargetRef

	return operation, nil
}

func validateDraftPROperation(envelope operationEnvelope, decision trust.Decision, policy config.M3Publication, operation Operation) (Operation, error) {
	var (
		preconditions struct {
			HeadRef          string `json:"head_ref"`
			PullRequestState string `json:"pull_request_state"`
			Draft            bool   `json:"draft"`
		}
		payload struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
	)

	expected := "refs/heads/" + policy.BranchPrefix + decision.CorrelationKey
	if decodeStrict(envelope.Preconditions, &preconditions) != nil || preconditions.HeadRef != expected || preconditions.PullRequestState != "absent" || !preconditions.Draft || decodeStrict(envelope.Payload, &payload) != nil || invalidText(payload.Title, policy.MaxPRTitleBytes) || invalidText(payload.Body, policy.MaxPRBodyBytes) {
		return Operation{}, ErrRejected
	}

	operation.TargetRef = preconditions.HeadRef
	operation.PRTitle = payload.Title
	operation.PRBody = payload.Body

	return operation, nil
}

func requestID(runID, kind, payloadDigest string) string {
	hash := sha256.New()
	hash.Write([]byte(runID))
	hash.Write([]byte{0})
	hash.Write([]byte(kind))
	hash.Write([]byte{0})
	hash.Write([]byte(payloadDigest))

	return hex.EncodeToString(hash.Sum(nil))
}

func invalidText(value string, maximum int) bool {
	return value == "" || len(value) > maximum || !utf8.ValidString(value)
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}

	_, err := hex.DecodeString(value)

	return err == nil
}

func validCommit(value string) bool {
	if len(value) != 40 {
		return false
	}

	_, err := hex.DecodeString(value)

	return err == nil
}

func validRef(value string) bool {
	return strings.HasPrefix(value, "refs/heads/") && len(value) > len("refs/heads/") && !strings.ContainsAny(value, "\x00\r\n")
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func decodeStrict(data []byte, value any) error {
	if len(data) == 0 || !utf8.Valid(data) {
		return ErrRejected
	}

	if err := validateJSON(data); err != nil {
		return err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decoding JSON: %w", err)
	}

	return nil
}

func validateJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	if err := validateJSONValue(decoder); err != nil {
		return err
	}

	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrRejected
	}

	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil || token == nil {
		return ErrRejected
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})

		for decoder.More() {
			key, keyOK := decoder.Token()

			name, nameOK := key.(string)
			if keyOK != nil || !nameOK {
				return ErrRejected
			}

			if _, duplicate := seen[name]; duplicate {
				return ErrRejected
			}

			seen[name] = struct{}{}

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
		return ErrRejected
	}

	closing, err := decoder.Token()
	if err != nil || closing != map[json.Delim]json.Delim{'{': '}', '[': ']'}[delimiter] {
		return ErrRejected
	}

	return nil
}
