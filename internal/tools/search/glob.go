// Package search implements glob and grep tools.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const maxGlobResults = 1000

// Glob finds files matching a pattern, supporting ** for recursive matching.
type Glob struct {
	WorkDir string
}

type globInput struct {
	Pattern string `json:"pattern"`
	BaseDir string `json:"base_dir"`
}

func (t *Glob) Name() string { return "glob" }
func (t *Glob) Description() string {
	return "Find files matching a glob pattern. Supports ** for recursive matching. " +
		"Examples: '**/*.go' finds all Go files recursively, 'src/*.ts' finds TypeScript files in src/, " +
		"'**/*_test.go' finds all Go test files. Returns up to 1000 matches."
}

func (t *Glob) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("pattern", toolutil.NewSchema("string").Desc("Glob pattern. Use ** for recursive directory matching. Examples: **/*.go, src/**/*.ts, *.md")).
		Prop("base_dir", toolutil.NewSchema("string").Desc("Directory to search in. Defaults to the working directory")).
		Required("pattern").
		Build()
}

func (t *Glob) NeedsPermission(_ json.RawMessage) bool { return false }

func (t *Glob) Execute(_ context.Context, rawInput json.RawMessage) (string, error) {
	var inp globInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("glob: bad input: %w", err)
	}

	baseDir := t.WorkDir
	if inp.BaseDir != "" {
		var err error
		baseDir, err = toolutil.SafePath(t.WorkDir, inp.BaseDir)
		if err != nil {
			return "", fmt.Errorf("glob: %w", err)
		}
	}

	matches, err := globPattern(baseDir, inp.Pattern)
	if err != nil {
		return "", fmt.Errorf("glob: %w", err)
	}

	if len(matches) == 0 {
		return fmt.Sprintf("no files matched pattern %q in %s", inp.Pattern, baseDir), nil
	}

	rel := make([]string, 0, len(matches))
	for _, m := range matches {
		r, err := filepath.Rel(baseDir, m)
		if err != nil {
			r = m
		}
		rel = append(rel, r)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) matched %q:\n", len(rel), inp.Pattern)
	for _, r := range rel {
		b.WriteString("  " + r + "\n")
	}
	return toolutil.Truncate(b.String()), nil
}

func globPattern(baseDir, pattern string) ([]string, error) {
	var matches []string
	pattern = filepath.FromSlash(pattern)

	err := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && !strings.Contains(pattern, "."+d.Name()) {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return nil
		}

		matched, err := matchDoublestar(pattern, rel)
		if err != nil || !matched {
			return nil
		}
		if !d.IsDir() {
			matches = append(matches, path)
		}
		if len(matches) >= maxGlobResults {
			return fmt.Errorf("glob limit reached")
		}
		return nil
	})

	if err != nil && err.Error() != "glob limit reached" {
		return nil, err
	}
	return matches, nil
}

func matchDoublestar(pattern, path string) (bool, error) {
	patSegs := splitPath(pattern)
	pathSegs := splitPath(path)
	return matchSegments(patSegs, pathSegs)
}

func matchSegments(patSegs, pathSegs []string) (bool, error) {
	for len(patSegs) > 0 && len(pathSegs) > 0 {
		p := patSegs[0]
		if p == "**" {
			if ok, _ := matchSegments(patSegs[1:], pathSegs); ok {
				return true, nil
			}
			return matchSegments(patSegs, pathSegs[1:])
		}
		ok, err := filepath.Match(p, pathSegs[0])
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		patSegs = patSegs[1:]
		pathSegs = pathSegs[1:]
	}

	for len(patSegs) > 0 && patSegs[0] == "**" {
		patSegs = patSegs[1:]
	}

	return len(patSegs) == 0 && len(pathSegs) == 0, nil
}

func splitPath(p string) []string {
	p = filepath.Clean(p)
	if p == "." {
		return nil
	}
	return strings.Split(p, string(filepath.Separator))
}

