package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

var (
	ErrWriteNotAllowed = errors.New("file write is not allowed")
	ErrWriteLimit      = errors.New("file write limit exceeded")
)

type WriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type WriteResult struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type Change struct {
	Path      string `json:"path"`
	OldSHA256 string `json:"old_sha256"`
	NewSHA256 string `json:"new_sha256"`
	Existed   bool   `json:"existed"`
}

type writtenFile struct {
	change      Change
	oldData     []byte
	oldMode     os.FileMode
	currentSize int
}

type Authoring struct {
	mu              sync.Mutex
	root            *os.Root
	allowed         []string
	maxChangedFiles int
	maxFileBytes    int
	maxTotalBytes   int
	totalBytes      int
	changes         map[string]writtenFile
	closed          bool
}

func NewAuthoring(root string, allowed []string, maxChangedFiles, maxFileBytes, maxTotalBytes int) (*Authoring, error) {
	if root == "" || len(allowed) == 0 || maxChangedFiles <= 0 || maxFileBytes <= 0 || maxTotalBytes <= 0 {
		return nil, ErrInvalidPolicy
	}

	opened, err := openRoot(root)
	if err != nil {
		return nil, err
	}

	return &Authoring{
		root: opened, allowed: slices.Clone(allowed), maxChangedFiles: maxChangedFiles,
		maxFileBytes: maxFileBytes, maxTotalBytes: maxTotalBytes, changes: make(map[string]writtenFile),
	}, nil
}

func (a *Authoring) Write(ctx context.Context, args WriteArgs) (*WriteResult, error) {
	name, err := a.validateWrite(ctx, args)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed || !a.allows(name) || len(args.Content) > a.maxFileBytes {
		return nil, ErrWriteNotAllowed
	}

	pathErr := a.checkPath(name)
	if pathErr != nil {
		return nil, pathErr
	}

	old, oldMode, existed, err := a.readExisting(name)
	if err != nil {
		return nil, err
	}

	newDigest := digest([]byte(args.Content))
	if existed && string(old) == args.Content {
		return &WriteResult{Path: name, Status: "unchanged", Size: len(args.Content), SHA256: newDigest}, nil
	}

	prior, alreadyChanged := a.changes[name]

	oldTrackedBytes, budgetErr := a.checkBudget(prior, alreadyChanged, len(args.Content))
	if budgetErr != nil {
		return nil, budgetErr
	}

	renamed, writeErr := a.atomicWrite(name, []byte(args.Content), oldMode)
	if renamed {
		if !alreadyChanged {
			prior = writtenFile{change: Change{Path: name, OldSHA256: digest(old), Existed: existed}, oldData: slices.Clone(old), oldMode: oldMode}
		}

		prior.change.NewSHA256 = newDigest
		prior.currentSize = len(args.Content)
		a.changes[name] = prior
		a.totalBytes = a.totalBytes - oldTrackedBytes + len(args.Content)
	}

	if writeErr != nil {
		return nil, writeErr
	}

	return &WriteResult{Path: name, Status: "applied", Size: len(args.Content), SHA256: newDigest}, nil
}

func (a *Authoring) ChangedPaths() []string {
	if a == nil {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	paths := make([]string, 0, len(a.changes))
	for name := range a.changes {
		paths = append(paths, name)
	}

	slices.Sort(paths)

	return paths
}

func (a *Authoring) Changes() []Change {
	if a == nil {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	changes := make([]Change, 0, len(a.changes))
	for _, value := range a.changes {
		changes = append(changes, value.change)
	}

	slices.SortFunc(changes, func(left, right Change) int { return strings.Compare(left.Path, right.Path) })

	return changes
}

func (a *Authoring) Rollback() error {
	if a == nil {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	paths := make([]string, 0, len(a.changes))
	for name := range a.changes {
		paths = append(paths, name)
	}

	slices.Sort(paths)

	for _, name := range paths {
		change := a.changes[name]
		if change.change.Existed {
			if _, err := a.atomicWrite(name, change.oldData, change.oldMode); err != nil {
				return fmt.Errorf("restoring authored file: %w", err)
			}

			continue
		}

		if err := a.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing authored file: %w", err)
		}
	}

	return nil
}

func (a *Authoring) Close() error {
	if a == nil {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return nil
	}

	a.closed = true

	if err := a.root.Close(); err != nil {
		return fmt.Errorf("closing authoring root: %w", err)
	}

	return nil
}

func (a *Authoring) validateWrite(ctx context.Context, args WriteArgs) (string, error) {
	if a == nil || !utf8.ValidString(args.Content) {
		return "", ErrWriteNotAllowed
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}

	return writePath(args.Path)
}

func (a *Authoring) checkBudget(prior writtenFile, alreadyChanged bool, contentBytes int) (int, error) {
	newCount := len(a.changes)
	oldTrackedBytes := 0

	if alreadyChanged {
		oldTrackedBytes = prior.currentSize
	} else {
		newCount++
	}

	if newCount > a.maxChangedFiles || a.totalBytes-oldTrackedBytes+contentBytes > a.maxTotalBytes {
		return 0, ErrWriteLimit
	}

	return oldTrackedBytes, nil
}

func (a *Authoring) allows(name string) bool {
	for _, allowed := range a.allowed {
		if strings.HasSuffix(allowed, "/") {
			if strings.HasPrefix(name, allowed) {
				return true
			}

			continue
		}

		if name == allowed {
			return true
		}
	}

	return false
}

func (a *Authoring) checkPath(name string) error {
	parts := strings.Split(name, "/")
	for index := range parts {
		current := strings.Join(parts[:index+1], "/")

		info, err := a.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && index == len(parts)-1 {
			return nil
		}

		if err != nil {
			return fmt.Errorf("checking authored path: %w", err)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return ErrWriteNotAllowed
		}

		if index < len(parts)-1 && !info.IsDir() {
			return ErrWriteNotAllowed
		}

		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return ErrWriteNotAllowed
		}
	}

	return nil
}

func (a *Authoring) readExisting(name string) (data []byte, mode os.FileMode, exists bool, readErr error) {
	info, err := a.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0o600, false, nil
	}

	if err != nil {
		return nil, 0, false, fmt.Errorf("stating authored file: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil, 0, false, ErrWriteNotAllowed
	}

	file, err := a.root.Open(name)
	if err != nil {
		return nil, 0, false, fmt.Errorf("opening authored file: %w", err)
	}

	opened, statErr := file.Stat()
	if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, 0, false, ErrWriteNotAllowed
	}

	data, readErr = io.ReadAll(io.LimitReader(file, int64(a.maxFileBytes)+1))
	closeErr := file.Close()

	if readErr != nil {
		return nil, 0, false, fmt.Errorf("reading authored file: %w", readErr)
	}

	if closeErr != nil {
		return nil, 0, false, fmt.Errorf("closing authored file: %w", closeErr)
	}

	if len(data) > a.maxFileBytes {
		return nil, 0, false, ErrWriteLimit
	}

	return data, info.Mode().Perm(), true, nil
}

func (a *Authoring) atomicWrite(name string, data []byte, mode os.FileMode) (bool, error) {
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(name)))
	if parent == "." {
		parent = ""
	}

	temporary, err := temporaryName(parent)
	if err != nil {
		return false, err
	}

	file, err := a.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return false, fmt.Errorf("creating authored temporary file: %w", err)
	}

	if chmodErr := file.Chmod(mode.Perm()); chmodErr != nil {
		_ = file.Close()
		_ = a.root.Remove(temporary)

		return false, fmt.Errorf("setting authored temporary file mode: %w", chmodErr)
	}

	cleanup := true

	defer func() {
		_ = file.Close()

		if cleanup {
			_ = a.root.Remove(temporary)
		}
	}()

	if _, writeErr := file.Write(data); writeErr != nil {
		return false, fmt.Errorf("writing authored temporary file: %w", writeErr)
	}

	if syncErr := file.Sync(); syncErr != nil {
		return false, fmt.Errorf("syncing authored temporary file: %w", syncErr)
	}

	if closeErr := file.Close(); closeErr != nil {
		return false, fmt.Errorf("closing authored temporary file: %w", closeErr)
	}

	pathErr := a.checkPath(name)
	if pathErr != nil && !errors.Is(pathErr, os.ErrNotExist) {
		return false, pathErr
	}

	if renameErr := a.root.Rename(temporary, name); renameErr != nil {
		return false, fmt.Errorf("renaming authored file: %w", renameErr)
	}

	cleanup = false

	if err := a.syncDirectory(parent); err != nil {
		return true, err
	}

	return true, nil
}

func (a *Authoring) syncDirectory(parent string) error {
	directory := "."
	if parent != "" {
		directory = parent
	}

	dir, err := a.root.Open(directory)
	if err != nil {
		return fmt.Errorf("opening authored directory: %w", err)
	}

	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("syncing authored directory: %w", err)
	}

	if err := dir.Close(); err != nil {
		return fmt.Errorf("closing authored directory: %w", err)
	}

	return nil
}

func writePath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\\\x00") || filepath.IsAbs(name) {
		return "", ErrWriteNotAllowed
	}

	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if cleaned != name || !filepath.IsLocal(filepath.FromSlash(cleaned)) || cleaned == ".git" || strings.HasPrefix(cleaned, ".git/") {
		return "", ErrWriteNotAllowed
	}

	return cleaned, nil
}

func temporaryName(parent string) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("reading temporary file randomness: %w", err)
	}

	name := ".duto-write-" + hex.EncodeToString(random[:])
	if parent != "" {
		name = parent + "/" + name
	}

	return name, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
