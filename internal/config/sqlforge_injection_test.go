package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadInjectsSQLForgeGuidance(t *testing.T) {
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "sqlforge.yml"), []byte("default_environment: dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".claude", "settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(proj)
	if err := l.Load(); err != nil {
		t.Fatal(err)
	}
	inj := l.SystemInjection()
	for _, want := range []string{"Drover SQLForge", "sqlforge plan dev", "sqlforge apply dev"} {
		if !strings.Contains(inj, want) {
			t.Fatalf("missing %q in injection:\n%s", want, inj)
		}
	}
}
