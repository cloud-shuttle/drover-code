// Package skills indexes SKILL.md files for the local agent.
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const maxSkills = 40
const maxBody = 32 * 1024

// Skill is one discoverable instruction file.
type Skill struct {
	Name        string
	Description string
	Path        string
}

// Discover lists skills under the workspace and ~/.drover/skills.
// The workspace copy wins when names collide.
func Discover(workDir string) []Skill {
	homeRoot := ""
	if home, err := os.UserHomeDir(); err == nil {
		homeRoot = filepath.Join(home, ".drover", "skills")
	}
	return discoverIn(workDir, homeRoot)
}

func discoverIn(workDir, homeRoot string) []Skill {
	var out []Skill
	seen := map[string]bool{}
	add := func(s Skill) {
		if s.Name == "" || seen[s.Name] || len(out) >= maxSkills {
			return
		}
		seen[s.Name] = true
		out = append(out, s)
	}
	walk := func(root string) {
		if root == "" {
			return
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				if path != root && skipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == "SKILL.md" {
				add(parseFile(path))
			}
			return nil
		})
	}
	walk(workDir)
	walk(homeRoot)
	return out
}

// Prompt is the system-prompt index. It is empty when no skills are installed.
func Prompt(workDir string) string {
	list := Discover(workDir)
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# SKILLS\n")
	b.WriteString("These skills are available. Call read_skill with the name before following one. The body is not loaded until you do.\n")
	for _, s := range list {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	return b.String()
}

// Read returns one skill body by name.
func Read(workDir, name string) (string, error) {
	return readFrom(Discover(workDir), name)
}

func readFrom(list []Skill, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("read_skill: name is required")
	}
	for _, s := range list {
		if s.Name != name {
			continue
		}
		b, err := os.ReadFile(s.Path)
		if err != nil {
			return "", err
		}
		body := string(b)
		if len(body) > maxBody {
			body = body[:maxBody] + "\n…(truncated)"
		}
		return body, nil
	}
	return "", fmt.Errorf("read_skill: %q is not in the skill index", name)
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", ".unikraft":
		return true
	default:
		return false
	}
}

func parseFile(path string) Skill {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}
	}
	name, desc := parseFront(string(b))
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	if desc == "" {
		desc = "SKILL.md"
	}
	return Skill{Name: name, Description: desc, Path: path}
}

func parseFront(text string) (name, desc string) {
	if !strings.HasPrefix(text, "---\n") {
		return "", ""
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", ""
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			name = val
		case "description":
			desc = val
		}
	}
	return name, desc
}

// ReadTool loads one skill body.
type ReadTool struct {
	WorkDir string
}

func (t *ReadTool) Name() string { return "read_skill" }
func (t *ReadTool) Description() string {
	return "Load the body of one skill from the session index by name. Use this when a task matches a listed skill."
}
func (t *ReadTool) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("name", toolutil.NewSchema("string").Desc("Skill name from the system prompt index")).
		Required("name").
		Build()
}
func (t *ReadTool) NeedsPermission(json.RawMessage) bool { return false }
func (t *ReadTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("read_skill: bad input: %w", err)
	}
	return Read(t.WorkDir, in.Name)
}
