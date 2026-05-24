package sqlforge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sqlforge.yml"), []byte("default_environment: staging\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "models", "staging")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	p, ok := FindProject(sub)
	if !ok {
		t.Fatal("expected project")
	}
	if p.Root != root {
		t.Errorf("root: got %q want %q", p.Root, root)
	}
	if p.DefaultEnvironment != "staging" {
		t.Errorf("env: got %q", p.DefaultEnvironment)
	}
}

func TestFindProjectMissing(t *testing.T) {
	root := t.TempDir()
	if _, ok := FindProject(root); ok {
		t.Fatal("expected no project")
	}
}
