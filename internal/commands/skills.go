package commands

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SkillDefinition is a progressive-disclosure recipe under .drover/skills/<name>/SKILL.md.
// Skills are reusable recipes, not permission or runtime enforcement.
type SkillDefinition struct {
	Name        string
	Description string
	Body        string
	FilePath    string
	LoadedAt    time.Time
}

// SkillRegistry holds loaded skills.
type SkillRegistry struct {
	skills map[string]SkillDefinition
}

// NewSkillRegistry creates an empty skill registry.
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{skills: make(map[string]SkillDefinition)}
}

// Register adds or replaces a skill.
func (r *SkillRegistry) Register(s SkillDefinition) {
	r.skills[s.Name] = s
}

// Get returns a skill by name.
func (r *SkillRegistry) Get(name string) (SkillDefinition, bool) {
	s, ok := r.skills[name]
	return s, ok
}

// List returns all skills.
func (r *SkillRegistry) List() []SkillDefinition {
	out := make([]SkillDefinition, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	return out
}

// LoadSkills loads .drover/skills/*/SKILL.md from project and ~/.drover/skills.
// This is an explicit skill adapter; command loading under .drover/commands is unchanged.
func (l *Loader) LoadSkills() (*SkillRegistry, error) {
	reg := NewSkillRegistry()
	dirs := []string{filepath.Join(l.workDir, ".drover", "skills")}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".drover", "skills"))
	}
	for _, root := range dirs {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return reg, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillPath := filepath.Join(root, e.Name(), "SKILL.md")
			s, err := parseSkillMarkdown(e.Name(), skillPath)
			if err != nil {
				continue
			}
			reg.Register(s)
		}
	}
	return reg, nil
}

func parseSkillMarkdown(name, path string) (SkillDefinition, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SkillDefinition{}, err
	}
	s := SkillDefinition{
		Name:     name,
		FilePath: path,
		LoadedAt: time.Now().UTC(),
		Body:     string(b),
	}
	// Optional YAML frontmatter
	content := string(b)
	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			meta := parts[1]
			s.Body = strings.TrimSpace(parts[2])
			sc := bufio.NewScanner(strings.NewReader(meta))
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if strings.HasPrefix(line, "name:") {
					s.Name = strings.TrimSpace(strings.Trim(strings.TrimPrefix(line, "name:"), `"'`))
				}
				if strings.HasPrefix(line, "description:") {
					s.Description = strings.TrimSpace(strings.Trim(strings.TrimPrefix(line, "description:"), `"'`))
				}
			}
		}
	}
	if s.Description == "" {
		// first non-empty markdown line as description fallback
		for _, line := range strings.Split(s.Body, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if line != "" {
				s.Description = line
				break
			}
		}
	}
	return s, nil
}

// AsCommand exposes a skill as a slash-command template for progressive disclosure.
func (s SkillDefinition) AsCommand() CommandDefinition {
	return CommandDefinition{
		Name:        "skill:" + s.Name,
		Description: s.Description,
		Template:    s.Body,
		Source:      "skill:" + s.FilePath,
		LoadedAt:    s.LoadedAt,
		FilePath:    s.FilePath,
	}
}
