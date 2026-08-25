package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/PedroKlein/duto-ai/internal/tool/files"
)

var (
	ErrAuthoringState = errors.New("unsafe repository authoring state")
	ErrCommitRequest  = errors.New("invalid Git commit request")
)

type AuthoringPolicy struct {
	Root                  string
	AllowedPaths          []string
	MaxChangedFiles       int
	MaxCommitMessageBytes int
	MaxRecoveryBytes      int
	AuthorName            string
	AuthorEmail           string
	BaseRef               string
	BaseSHA               string
	EvidenceDirectory     string
}

type CommitArgs struct {
	Paths   []string `json:"paths"`
	Message string   `json:"message"`
}

type CommitResult struct {
	Status string   `json:"status"`
	Commit string   `json:"commit"`
	Tree   string   `json:"tree"`
	Paths  []string `json:"paths"`
}

type Authoring struct {
	mu        sync.Mutex
	policy    AuthoringPolicy
	writer    *files.Authoring
	remotes   []byte
	refs      []byte
	config    []byte
	submodule []byte
	committed bool
	commit    string
}

type repositorySnapshot struct {
	remotes   []byte
	refs      []byte
	config    []byte
	submodule []byte
}

func NewAuthoring(ctx context.Context, policy AuthoringPolicy) (*Authoring, error) {
	if policy.Root == "" || policy.BaseRef == "" || policy.BaseSHA == "" || len(policy.AllowedPaths) == 0 ||
		policy.MaxChangedFiles <= 0 || policy.MaxCommitMessageBytes <= 0 || policy.MaxRecoveryBytes <= 0 {
		return nil, ErrInvalidPolicy
	}

	if err := verifyInitialRepository(ctx, policy); err != nil {
		return nil, err
	}

	snapshot, err := captureRepository(ctx, policy.Root)
	if err != nil {
		return nil, err
	}

	return &Authoring{
		policy: policy, remotes: snapshot.remotes, refs: snapshot.refs,
		config: snapshot.config, submodule: snapshot.submodule,
	}, nil
}

func verifyInitialRepository(ctx context.Context, policy AuthoringPolicy) error {
	head, err := gitOutput(ctx, policy.Root, nil, "rev-parse", "HEAD")
	if err != nil {
		return err
	}

	if strings.TrimSpace(string(head)) != policy.BaseSHA {
		return fmt.Errorf("checkout HEAD does not match attested base: %w", ErrAuthoringState)
	}

	ref, err := gitOutput(ctx, policy.Root, nil, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return err
	}

	if strings.TrimSpace(string(ref)) != policy.BaseRef {
		return fmt.Errorf("checkout ref does not match attested base: %w", ErrAuthoringState)
	}

	status, err := gitOutput(ctx, policy.Root, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}

	if len(status) != 0 {
		if hasIndexChanges(status) {
			return fmt.Errorf("git index must be clean: %w", ErrAuthoringState)
		}

		return fmt.Errorf("git worktree must be clean: %w", ErrAuthoringState)
	}

	return verifyAllowedPaths(ctx, policy)
}

func verifyAllowedPaths(ctx context.Context, policy AuthoringPolicy) error {
	for _, allowed := range policy.AllowedPaths {
		ignored, err := gitExit(ctx, policy.Root, "check-ignore", "--no-index", "--", strings.TrimSuffix(allowed, "/"))
		if err != nil {
			return err
		}

		if ignored {
			return fmt.Errorf("selected allowed path is ignored: %w", ErrAuthoringState)
		}
	}

	submodules, err := gitOutput(ctx, policy.Root, nil, "ls-files", "--stage")
	if err != nil {
		return err
	}

	if selectedSubmodule(policy.AllowedPaths, submodules) {
		return fmt.Errorf("selected allowed path crosses a submodule: %w", ErrAuthoringState)
	}

	return nil
}

func captureRepository(ctx context.Context, root string) (repositorySnapshot, error) {
	remotes, err := gitOutput(ctx, root, nil, "remote", "-v")
	if err != nil {
		return repositorySnapshot{}, err
	}

	refs, err := gitOutput(ctx, root, nil, "for-each-ref", "--format=%(refname)%00%(objectname)", "refs/heads", "refs/tags", "refs/remotes")
	if err != nil {
		return repositorySnapshot{}, err
	}

	config, err := gitOutput(ctx, root, nil, "config", "--local", "--null", "--list")
	if err != nil {
		return repositorySnapshot{}, err
	}

	submodules, err := gitOutput(ctx, root, nil, "ls-files", "--stage")
	if err != nil {
		return repositorySnapshot{}, err
	}

	return repositorySnapshot{remotes: remotes, refs: refs, config: config, submodule: submoduleEntries(submodules)}, nil
}

func (a *Authoring) BindWriter(writer *files.Authoring) error {
	if a == nil || writer == nil {
		return ErrInvalidPolicy
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.writer != nil {
		return ErrInvalidPolicy
	}

	a.writer = writer

	return nil
}

func (a *Authoring) Commit(ctx context.Context, args CommitArgs) (*CommitResult, error) {
	if a == nil {
		return nil, ErrCommitRequest
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.writer == nil || a.committed || invalidCommitMessage(args.Message, a.policy.MaxCommitMessageBytes) {
		return nil, ErrCommitRequest
	}

	paths, err := normalizedCommitPaths(args.Paths, a.policy.MaxChangedFiles)
	if err != nil {
		return nil, err
	}

	changed := a.writer.ChangedPaths()
	if !slices.Equal(paths, changed) {
		return nil, ErrCommitRequest
	}

	if len(paths) == 0 {
		return a.unchangedCommit(ctx)
	}

	return a.createCommit(ctx, paths, args.Message)
}

func (a *Authoring) Verify(ctx context.Context) error {
	if a == nil {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.writer == nil {
		return ErrInvalidPolicy
	}

	if len(a.writer.ChangedPaths()) > 0 && !a.committed {
		return fmt.Errorf("authored files were not committed: %w", ErrAuthoringState)
	}

	expected := a.policy.BaseSHA
	if a.committed {
		expected = a.commit
	}

	return a.verifyRepository(ctx, expected)
}

func (a *Authoring) Recover(ctx context.Context, failureKind string) error {
	if a == nil || a.writer == nil || (len(a.writer.ChangedPaths()) == 0 && !a.committed) {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	paths := a.writer.ChangedPaths()

	patch, err := a.recoveryPatch(ctx, paths)
	if err != nil {
		return err
	}

	if len(patch) > a.policy.MaxRecoveryBytes {
		return fmt.Errorf("recovery patch exceeds byte limit: %w", ErrAuthoringState)
	}

	if err := a.writeRecovery(failureKind, patch); err != nil {
		return err
	}

	_, resetErr := gitOutput(ctx, a.policy.Root, nil, "reset", "--hard", a.policy.BaseSHA)
	if resetErr != nil {
		return fmt.Errorf("restoring authored checkout: %w", resetErr)
	}

	if rollbackErr := a.writer.Rollback(); rollbackErr != nil {
		return fmt.Errorf("rolling back authored files: %w", rollbackErr)
	}

	return a.verifyRepository(ctx, a.policy.BaseSHA)
}

func (a *Authoring) unchangedCommit(ctx context.Context) (*CommitResult, error) {
	tree, err := a.tree(ctx, a.policy.BaseSHA)
	if err != nil {
		return nil, err
	}

	return &CommitResult{Status: "unchanged", Commit: a.policy.BaseSHA, Tree: tree, Paths: []string{}}, nil
}

func (a *Authoring) createCommit(ctx context.Context, paths []string, message string) (*CommitResult, error) {
	verifyErr := a.verifyBeforeCommit(ctx, paths)
	if verifyErr != nil {
		return nil, verifyErr
	}

	for _, name := range paths {
		_, stageErr := gitOutput(ctx, a.policy.Root, nil, literalPathspecs, "add", "--", name)
		if stageErr != nil {
			return nil, fmt.Errorf("staging authored path: %w", stageErr)
		}
	}

	environment, err := a.commitEnvironment(ctx)
	if err != nil {
		return nil, err
	}

	_, commitErr := gitOutput(ctx, a.policy.Root, environment, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false", "commit", "--no-verify", "--no-gpg-sign", "-m", message)
	if commitErr != nil {
		return nil, fmt.Errorf("creating authored commit: %w", commitErr)
	}

	commitBytes, err := gitOutput(ctx, a.policy.Root, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}

	commit := strings.TrimSpace(string(commitBytes))
	a.committed = true
	a.commit = commit

	verifyErr = a.verifyAfterCommit(ctx, commit, paths)
	if verifyErr != nil {
		return nil, verifyErr
	}

	tree, err := a.tree(ctx, commit)
	if err != nil {
		return nil, err
	}

	return &CommitResult{Status: "applied", Commit: commit, Tree: tree, Paths: slices.Clone(paths)}, nil
}

func (a *Authoring) commitEnvironment(ctx context.Context) (map[string]string, error) {
	baseTime, err := gitOutput(ctx, a.policy.Root, nil, "show", "-s", "--format=%ct", a.policy.BaseSHA)
	if err != nil {
		return nil, err
	}

	seconds, err := strconv.ParseInt(strings.TrimSpace(string(baseTime)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing base commit time: %w", err)
	}

	date := strconv.FormatInt(seconds+1, 10) + " +0000"

	return map[string]string{
		"GIT_AUTHOR_NAME": a.policy.AuthorName, "GIT_AUTHOR_EMAIL": a.policy.AuthorEmail, "GIT_AUTHOR_DATE": date,
		"GIT_COMMITTER_NAME": a.policy.AuthorName, "GIT_COMMITTER_EMAIL": a.policy.AuthorEmail, "GIT_COMMITTER_DATE": date,
	}, nil
}

func (a *Authoring) recoveryPatch(ctx context.Context, paths []string) ([]byte, error) {
	arguments := []string{noPager, literalPathspecs, "diff", "--binary", "--no-ext-diff"}
	if a.committed {
		arguments = append(arguments, a.policy.BaseSHA, a.commit)
	} else {
		for _, name := range paths {
			_, err := gitOutput(ctx, a.policy.Root, nil, literalPathspecs, "add", "--", name)
			if err != nil {
				return nil, fmt.Errorf("staging recovery path: %w", err)
			}
		}

		arguments = append(arguments, "--cached", a.policy.BaseSHA)
	}

	arguments = append(arguments, "--")
	arguments = append(arguments, paths...)

	return gitOutput(ctx, a.policy.Root, nil, arguments...)
}

func (a *Authoring) writeRecovery(failureKind string, patch []byte) error {
	metadata := struct {
		Version     int            `json:"version"`
		BaseSHA     string         `json:"base_sha"`
		SourceSHA   string         `json:"source_sha"`
		FailureKind string         `json:"failure_kind"`
		Ordering    []string       `json:"ordering"`
		Changes     []files.Change `json:"changes"`
	}{
		Version: 1, BaseSHA: a.policy.BaseSHA, SourceSHA: a.commit, FailureKind: failureKind,
		Ordering: []string{"metadata_closed", "patch_closed", "cleanup_started"}, Changes: a.writer.Changes(),
	}

	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encoding recovery metadata: %w", err)
	}

	encoded = append(encoded, '\n')

	recovery := filepath.Join(a.policy.EvidenceDirectory, "recovery")
	if err := os.MkdirAll(recovery, 0o700); err != nil {
		return fmt.Errorf("creating recovery directory: %w", err)
	}

	if err := atomicHostWrite(filepath.Join(recovery, "metadata.json"), encoded); err != nil {
		return err
	}

	if err := atomicHostWrite(filepath.Join(recovery, "changes.patch"), patch); err != nil {
		return err
	}

	return syncDirectory(recovery)
}

func (a *Authoring) verifyBeforeCommit(ctx context.Context, paths []string) error {
	head, err := gitOutput(ctx, a.policy.Root, nil, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != a.policy.BaseSHA {
		return fmt.Errorf("authored commit base changed: %w", ErrAuthoringState)
	}

	status, err := gitOutput(ctx, a.policy.Root, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return err
	}

	observed, err := unstagedPaths(status)
	if err != nil || !slices.Equal(observed, paths) {
		return fmt.Errorf("worktree contains unrelated changes: %w", ErrAuthoringState)
	}

	for _, name := range paths {
		ignored, checkErr := gitExit(ctx, a.policy.Root, "check-ignore", "--no-index", "--", name)
		if checkErr != nil {
			return checkErr
		}

		if ignored {
			return fmt.Errorf("authored path is ignored: %w", ErrAuthoringState)
		}

		attributes, attrErr := gitOutput(ctx, a.policy.Root, nil, "check-attr", "filter", "--", name)
		if attrErr != nil {
			return attrErr
		}

		if !strings.HasSuffix(strings.TrimSpace(string(attributes)), ": unspecified") {
			return fmt.Errorf("authored path selects a Git filter: %w", ErrAuthoringState)
		}
	}

	return nil
}

func (a *Authoring) verifyAfterCommit(ctx context.Context, commit string, paths []string) error {
	parents, err := gitOutput(ctx, a.policy.Root, nil, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return err
	}

	fields := strings.Fields(string(parents))
	if len(fields) != 2 || fields[1] != a.policy.BaseSHA {
		return fmt.Errorf("authored commit parent changed: %w", ErrAuthoringState)
	}

	changed, err := gitOutput(ctx, a.policy.Root, nil, "diff-tree", "--no-commit-id", "--name-only", "-r", "--root", commit)
	if err != nil {
		return err
	}

	actual := nonemptyLines(changed)
	slices.Sort(actual)

	if !slices.Equal(actual, paths) {
		return fmt.Errorf("authored commit changed unexpected paths: %w", ErrAuthoringState)
	}

	return a.verifyRepository(ctx, commit)
}

func (a *Authoring) verifyRepository(ctx context.Context, expectedHead string) error {
	head, err := gitOutput(ctx, a.policy.Root, nil, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != expectedHead {
		return fmt.Errorf("authored repository HEAD changed: %w", ErrAuthoringState)
	}

	status, err := gitOutput(ctx, a.policy.Root, nil, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || len(status) != 0 {
		return fmt.Errorf("authored repository is not clean: %w", ErrAuthoringState)
	}

	remotesErr := a.compareSnapshot(ctx, "remotes", a.remotes, "remote", "-v")
	if remotesErr != nil {
		return remotesErr
	}

	configErr := a.compareSnapshot(ctx, "configuration", a.config, "config", "--local", "--null", "--list")
	if configErr != nil {
		return configErr
	}

	submodules, err := gitOutput(ctx, a.policy.Root, nil, "ls-files", "--stage")
	if err != nil {
		return err
	}

	if !bytes.Equal(a.submodule, submoduleEntries(submodules)) {
		return fmt.Errorf("git submodules changed: %w", ErrAuthoringState)
	}

	refs, err := gitOutput(ctx, a.policy.Root, nil, "for-each-ref", "--format=%(refname)%00%(objectname)", "refs/heads", "refs/tags", "refs/remotes")
	if err != nil {
		return err
	}

	if !sameRefsExcept(a.refs, refs, a.policy.BaseRef, expectedHead) {
		return fmt.Errorf("non-HEAD refs changed: %w", ErrAuthoringState)
	}

	return nil
}

func (a *Authoring) compareSnapshot(ctx context.Context, name string, expected []byte, args ...string) error {
	actual, err := gitOutput(ctx, a.policy.Root, nil, args...)
	if err != nil {
		return err
	}

	if !bytes.Equal(expected, actual) {
		return fmt.Errorf("git %s changed: %w", name, ErrAuthoringState)
	}

	return nil
}

func (a *Authoring) tree(ctx context.Context, commit string) (string, error) {
	value, err := gitOutput(ctx, a.policy.Root, nil, "rev-parse", commit+"^{tree}")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(value)), nil
}

func normalizedCommitPaths(source []string, maximum int) ([]string, error) {
	if len(source) == 0 || len(source) > maximum {
		return nil, ErrCommitRequest
	}

	paths := slices.Clone(source)
	slices.Sort(paths)

	for index, name := range paths {
		if name == "" || filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name || !filepath.IsLocal(filepath.FromSlash(name)) ||
			(index > 0 && paths[index-1] == name) {
			return nil, ErrCommitRequest
		}
	}

	return paths, nil
}

func invalidCommitMessage(value string, maximum int) bool {
	return value == "" || len(value) > maximum || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool {
		return r == 0 || r == '\r' || unicode.IsControl(r) && r != '\n' && r != '\t'
	}) >= 0
}

func hasIndexChanges(status []byte) bool {
	for line := range bytes.SplitSeq(status, []byte{'\n'}) {
		if len(line) >= 2 && line[0] != ' ' && line[0] != '?' {
			return true
		}
	}

	return false
}

func selectedSubmodule(allowed []string, staged []byte) bool {
	for line := range bytes.SplitSeq(submoduleEntries(staged), []byte{'\n'}) {
		fields := strings.Fields(string(line))
		if len(fields) < 4 || fields[0] != "160000" {
			continue
		}

		submodule := fields[3]

		for _, candidate := range allowed {
			candidate = strings.TrimSuffix(candidate, "/")
			if candidate == submodule || strings.HasPrefix(candidate, submodule+"/") || strings.HasPrefix(submodule, candidate+"/") {
				return true
			}
		}
	}

	return false
}

func submoduleEntries(staged []byte) []byte {
	var result []byte

	for line := range bytes.SplitSeq(staged, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("160000 ")) {
			result = append(result, line...)
			result = append(result, '\n')
		}
	}

	return result
}

func unstagedPaths(status []byte) ([]string, error) {
	records := bytes.Split(status, []byte{0})

	paths := make([]string, 0, len(records))
	for _, record := range records {
		if len(record) == 0 {
			continue
		}

		if len(record) < 4 || record[2] != ' ' || (record[0] != ' ' && record[0] != '?') {
			return nil, ErrAuthoringState
		}

		name := string(record[3:])
		if record[0] == '?' && record[1] == '?' {
			name = string(record[3:])
		}

		paths = append(paths, filepath.ToSlash(name))
	}

	slices.Sort(paths)

	return paths, nil
}

func nonemptyLines(data []byte) []string {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}
	}

	return lines
}

func sameRefsExcept(before, after []byte, currentRef, expected string) bool {
	parse := func(data []byte) map[string]string {
		result := make(map[string]string)

		for line := range bytes.SplitSeq(data, []byte{'\n'}) {
			parts := bytes.SplitN(line, []byte{0}, 2)
			if len(parts) == 2 {
				result[string(parts[0])] = string(parts[1])
			}
		}

		return result
	}
	left, right := parse(before), parse(after)
	delete(left, currentRef)
	current := right[currentRef]
	delete(right, currentRef)

	return current == expected && mapsEqual(left, right)
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}

	for key, value := range left {
		if right[key] != value {
			return false
		}
	}

	return true
}

func gitExit(ctx context.Context, root string, args ...string) (bool, error) {
	command := gitCommand(ctx, root, nil, args...)

	err := command.Run()
	if err == nil {
		return true, nil
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("checking Git path policy: %w", err)
}

func gitOutput(ctx context.Context, root string, extra map[string]string, args ...string) ([]byte, error) {
	command := gitCommand(ctx, root, extra, args...)

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("running fixed Git authoring command: %w", err)
	}

	if len(output) > 16<<20 {
		return nil, ErrAuthoringState
	}

	return output, nil
}

func gitCommand(ctx context.Context, root string, extra map[string]string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", args...) //nolint:gosec // argv is fixed by this package
	command.Dir = root

	command.Env = []string{
		"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0",
	}
	for key, value := range extra {
		command.Env = append(command.Env, key+"="+value)
	}

	return command
}

func atomicHostWrite(name string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(name), ".duto-recovery-")
	if err != nil {
		return fmt.Errorf("creating recovery temporary file: %w", err)
	}

	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting recovery file mode: %w", err)
	}

	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing recovery file: %w", err)
	}

	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("syncing recovery file: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing recovery file: %w", err)
	}

	if err := os.Rename(temporaryName, name); err != nil {
		return fmt.Errorf("renaming recovery file: %w", err)
	}

	return nil
}

func syncDirectory(name string) error {
	directory, err := os.Open(name) //nolint:gosec // trusted evidence directory
	if err != nil {
		return fmt.Errorf("opening recovery directory: %w", err)
	}
	defer directory.Close() //nolint:errcheck // best effort after explicit sync

	if err := directory.Sync(); err != nil {
		return fmt.Errorf("syncing recovery directory: %w", err)
	}

	return nil
}
