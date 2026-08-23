package prompt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	adkskill "google.golang.org/adk/v2/tool/skilltoolset/skill"
)

const (
	maxSkillFileBytes = 256 << 10
	maxSkillBytes     = 1 << 20
)

type SkillRequest struct {
	Workspace string
	Path      string
}

type FrozenSkill struct {
	Name      string      `json:"name"`
	Workspace string      `json:"workspace"`
	Path      string      `json:"path"`
	Digest    string      `json:"digest"`
	Files     []SkillFile `json:"files"`
}

type SkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Digest  string `json:"digest"`
}

func FreezeSkills(requests map[string]SkillRequest, workspaces map[string]string) ([]FrozenSkill, error) {
	names := make([]string, 0, len(requests))
	for name := range requests {
		names = append(names, name)
	}

	sort.Strings(names)

	result := make([]FrozenSkill, 0, len(names))
	for _, name := range names {
		frozen, err := freezeSkill(name, requests[name], workspaces)
		if err != nil {
			return nil, fmt.Errorf("freezing skill %q: %w", name, err)
		}

		result = append(result, frozen)
	}

	return result, nil
}

func freezeSkill(name string, request SkillRequest, workspaces map[string]string) (FrozenSkill, error) {
	rootPath, ok := workspaces[request.Workspace]
	if !ok || rootPath == "" {
		return FrozenSkill{}, ErrUnknownWorkspace
	}

	if request.Path == "" || path.IsAbs(request.Path) || path.Clean(request.Path) != request.Path {
		return FrozenSkill{}, adkskill.ErrInvalidSkillName
	}

	workspace, err := os.OpenRoot(rootPath)
	if err != nil {
		return FrozenSkill{}, fmt.Errorf("opening skill workspace: %w", err)
	}
	defer func() { _ = workspace.Close() }()

	skillRoot, err := workspace.OpenRoot(request.Path)
	if err != nil {
		return FrozenSkill{}, fmt.Errorf("opening skill directory: %w", err)
	}
	defer func() { _ = skillRoot.Close() }()

	files, err := readSkillFiles(skillRoot)
	if err != nil {
		return FrozenSkill{}, err
	}

	skillFile, ok := skillFileByPath(files, "SKILL.md")
	if !ok {
		return FrozenSkill{}, adkskill.ErrSkillNotFound
	}

	frontmatter, _, err := adkskill.ParseBytes([]byte(skillFile.Content))
	if err != nil {
		return FrozenSkill{}, fmt.Errorf("%w: %w", adkskill.ErrInvalidFrontmatter, err)
	}

	if frontmatter.Name != name {
		return FrozenSkill{}, adkskill.ErrInvalidSkillName
	}

	hasher := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(hasher, file.Path)
		_, _ = io.WriteString(hasher, file.Digest)
	}

	return FrozenSkill{
		Name:      name,
		Workspace: request.Workspace,
		Path:      request.Path,
		Digest:    hex.EncodeToString(hasher.Sum(nil)),
		Files:     files,
	}, nil
}

func readSkillFiles(root *os.Root) ([]SkillFile, error) {
	reader := skillFilesReader{root: root}
	if err := fs.WalkDir(root.FS(), ".", reader.visit); err != nil {
		return nil, fmt.Errorf("reading skill files: %w", err)
	}

	sort.Slice(reader.files, func(i, j int) bool { return reader.files[i].Path < reader.files[j].Path })

	return reader.files, nil
}

type skillFilesReader struct {
	root  *os.Root
	files []SkillFile
	total int
}

func (r *skillFilesReader) visit(filePath string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return fmt.Errorf("walking skill resource: %w", walkErr)
	}

	if entry.Type()&fs.ModeSymlink != 0 {
		return adkskill.ErrInvalidResourcePath
	}

	if entry.IsDir() {
		return skillDirectoryDisposition(filePath)
	}

	if filePath != "SKILL.md" && !allowedResourcePath(filePath) {
		return nil
	}

	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("stating skill resource: %w", err)
	}

	if !info.Mode().IsRegular() || info.Size() > maxSkillFileBytes {
		return adkskill.ErrInvalidResourcePath
	}

	body, err := readRootFile(r.root, filePath, maxSkillFileBytes)
	if err != nil {
		return err
	}

	r.total += len(body)
	if r.total > maxSkillBytes || !utf8.Valid(body) {
		return adkskill.ErrInvalidResourcePath
	}

	digest := sha256.Sum256(body)
	r.files = append(r.files, SkillFile{Path: filePath, Content: string(body), Digest: hex.EncodeToString(digest[:])})

	return nil
}

func skillDirectoryDisposition(filePath string) error {
	switch filePath {
	case ".", "references", "assets", "scripts":
		return nil
	default:
		return fs.SkipDir
	}
}

func readRootFile(root *os.Root, filePath string, maxBytes int) ([]byte, error) {
	file, err := root.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening skill resource: %w", err)
	}
	defer func() { _ = file.Close() }()

	body, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("reading skill resource: %w", err)
	}

	if len(body) > maxBytes {
		return nil, ErrSourceBounds
	}

	return body, nil
}

func allowedResourcePath(filePath string) bool {
	return strings.HasPrefix(filePath, "references/") || strings.HasPrefix(filePath, "assets/") || strings.HasPrefix(filePath, "scripts/")
}

func skillFileByPath(files []SkillFile, filePath string) (SkillFile, bool) {
	for _, file := range files {
		if file.Path == filePath {
			return file, true
		}
	}

	return SkillFile{}, false
}

type restrictedSkillSource struct {
	skills map[string]FrozenSkill
}

func NewSkillSource(skills []FrozenSkill, selected []string) adkskill.Source {
	available := make(map[string]FrozenSkill, len(selected))
	for _, name := range selected {
		for _, frozen := range skills {
			if frozen.Name == name {
				available[name] = frozen
				break
			}
		}
	}

	return &restrictedSkillSource{skills: available}
}

func (s *restrictedSkillSource) ListFrontmatters(ctx context.Context) ([]*adkskill.Frontmatter, error) {
	names := make([]string, 0, len(s.skills))
	for name := range s.skills {
		names = append(names, name)
	}

	sort.Strings(names)

	frontmatters := make([]*adkskill.Frontmatter, 0, len(names))
	for _, name := range names {
		frontmatter, err := s.LoadFrontmatter(ctx, name)
		if err != nil {
			return nil, err
		}

		frontmatters = append(frontmatters, frontmatter)
	}

	return frontmatters, nil
}

func (s *restrictedSkillSource) LoadFrontmatter(_ context.Context, name string) (*adkskill.Frontmatter, error) {
	frozen, ok := s.skills[name]
	if !ok {
		return nil, adkskill.ErrSkillNotFound
	}

	file, ok := skillFileByPath(frozen.Files, "SKILL.md")
	if !ok {
		return nil, adkskill.ErrSkillNotFound
	}

	frontmatter, _, err := adkskill.ParseBytes([]byte(file.Content))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", adkskill.ErrInvalidFrontmatter, err)
	}

	return frontmatter, nil
}

func (s *restrictedSkillSource) LoadInstructions(_ context.Context, name string) (string, error) {
	frozen, ok := s.skills[name]
	if !ok {
		return "", adkskill.ErrSkillNotFound
	}

	file, ok := skillFileByPath(frozen.Files, "SKILL.md")
	if !ok {
		return "", adkskill.ErrSkillNotFound
	}

	_, instructions, err := adkskill.ParseBytes([]byte(file.Content))
	if err != nil {
		return "", fmt.Errorf("%w: %w", adkskill.ErrInvalidFrontmatter, err)
	}

	return instructions, nil
}

func (s *restrictedSkillSource) ListResources(_ context.Context, name, subpath string) ([]string, error) {
	frozen, ok := s.skills[name]
	if !ok {
		return nil, adkskill.ErrSkillNotFound
	}

	clean := path.Clean(subpath)
	if clean != "." && !allowedResourcePath(clean+"/") && !allowedResourcePath(clean) {
		return nil, adkskill.ErrInvalidResourcePath
	}

	var resources []string

	for _, file := range frozen.Files {
		if file.Path == "SKILL.md" {
			continue
		}

		if clean == "." || file.Path == clean || strings.HasPrefix(file.Path, clean+"/") {
			resources = append(resources, file.Path)
		}
	}

	if len(resources) == 0 && clean != "." {
		return nil, adkskill.ErrResourceNotFound
	}

	return resources, nil
}

func (s *restrictedSkillSource) LoadResource(_ context.Context, name, resourcePath string) (io.ReadCloser, error) {
	frozen, ok := s.skills[name]
	if !ok {
		return nil, adkskill.ErrSkillNotFound
	}

	clean := path.Clean(resourcePath)
	if clean != resourcePath || !allowedResourcePath(clean) {
		return nil, adkskill.ErrInvalidResourcePath
	}

	file, ok := skillFileByPath(frozen.Files, clean)
	if !ok {
		return nil, adkskill.ErrResourceNotFound
	}

	return io.NopCloser(bytes.NewReader([]byte(file.Content))), nil
}

var _ adkskill.Source = (*restrictedSkillSource)(nil)
