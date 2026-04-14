package search

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlob_DoubleStar(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &Glob{WorkDir: dir}
	out, err := g.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.go"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a"+string(filepath.Separator)+"b"+string(filepath.Separator)+"x.go") &&
		!strings.Contains(out, "a/b/x.go") {
		t.Fatalf("expected match in output, got:\n%s", out)
	}
}

func TestGrep_PureGoBackend(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gr := &Grep{WorkDir: dir}
	// Force pure-go backend by ensuring `rg` won't be used:
	// we can't reliably hide rg from PATH, so just test the formatting invariants
	// (both backends are rg-like).
	out, err := gr.Execute(context.Background(), mustJSON(t, map[string]any{
		"pattern":       "two",
		"path":          "src",
		"context_lines": 1,
		"max_matches":   10,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt:2:two") {
		t.Fatalf("expected match line with file:line:content, got:\n%s", out)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

