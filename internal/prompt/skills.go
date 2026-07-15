package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultSkillsDir is the conventional directory for skill files.
const DefaultSkillsDir = ".github/ai-workflows/skills"

// SkillsRegistry maps skill names to their file paths.
type SkillsRegistry struct {
	mu     sync.RWMutex
	skills map[string]string
}

// NewSkillsRegistry creates a registry and auto-discovers skills from the default directory.
func NewSkillsRegistry() *SkillsRegistry {
	return NewSkillsRegistryFromDir(DefaultSkillsDir)
}

// NewSkillsRegistryFromDir creates a registry and discovers skills from a custom directory.
func NewSkillsRegistryFromDir(dir string) *SkillsRegistry {
	reg := &SkillsRegistry{
		skills: make(map[string]string),
	}

	reg.discover(dir)

	return reg
}

// Resolve returns the file path for a skill reference.
// Resolution order:
//  1. Exact path (if the reference is a direct file path that exists)
//  2. Auto-discovered name from the registry
//  3. Fallback to .github/ai-workflows/skills/<name>.md
func (r *SkillsRegistry) Resolve(ref string) string {
	// Check if the reference is a direct file path.
	if _, err := os.Stat(ref); err == nil {
		return ref
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check auto-discovered skills.
	if path, ok := r.skills[ref]; ok {
		return path
	}

	// Fallback to conventional path.
	return filepath.Join(DefaultSkillsDir, ref+".md")
}

// Names returns all discovered skill names.
func (r *SkillsRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}

	return names
}

// discover scans a directory for .md files and registers them by base name.
func (r *SkillsRegistry) discover(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory doesn't exist — no skills to discover
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}

		// Register without extension as the skill name.
		skillName := strings.TrimSuffix(name, filepath.Ext(name))
		r.skills[skillName] = filepath.Join(dir, name)
	}
}
