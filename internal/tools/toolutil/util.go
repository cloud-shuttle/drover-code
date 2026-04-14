package toolutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// MaxOutputBytes is the maximum number of bytes returned from any tool.
// Outputs larger than this are truncated with a note so the model knows
// there is more content.
const MaxOutputBytes = 200_000

// Truncate clips s to MaxOutputBytes if it exceeds that limit.
func Truncate(s string) string {
	if len(s) <= MaxOutputBytes {
		return s
	}
	// Clip at a valid UTF-8 boundary.
	b := []byte(s[:MaxOutputBytes])
	for !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b) + fmt.Sprintf("\n\n[output truncated at %d bytes — %d bytes total]",
		MaxOutputBytes, len(s))
}

// WriteAtomic writes data to path atomically: write to a temp file in the
// same directory, then rename.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".drover-code-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	return nil
}

// SafePath resolves path against workDir and verifies it stays within workDir.
func SafePath(workDir, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if workDir != "" {
		absWork, err := filepath.Abs(workDir)
		if err != nil {
			return "", fmt.Errorf("resolve workdir: %w", err)
		}
		if !strings.HasPrefix(abs, absWork+string(filepath.Separator)) && abs != absWork {
			return "", fmt.Errorf("path %q escapes working directory %q", path, workDir)
		}
	}
	return abs, nil
}

// Schema is a convenience builder for JSON Schema objects sent to the API.
type Schema struct {
	m map[string]any
}

func NewSchema(typ string) *Schema {
	return &Schema{m: map[string]any{"type": typ}}
}

func (s *Schema) Desc(d string) *Schema {
	s.m["description"] = d
	return s
}

func (s *Schema) Prop(name string, child *Schema) *Schema {
	props, _ := s.m["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		s.m["properties"] = props
	}
	props[name] = child.m
	return s
}

func (s *Schema) Required(names ...string) *Schema {
	req, _ := s.m["required"].([]string)
	s.m["required"] = append(req, names...)
	return s
}

func (s *Schema) Enum(vals ...string) *Schema {
	anyVals := make([]any, len(vals))
	for i, v := range vals {
		anyVals[i] = v
	}
	s.m["enum"] = anyVals
	return s
}

func (s *Schema) Items(child *Schema) *Schema {
	s.m["items"] = child.m
	return s
}

func (s *Schema) Build() json.RawMessage {
	b, _ := json.Marshal(s.m)
	return b
}

