package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoader_MergeOrder_ProjectWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".drover", "settings.json"), []byte(`{"model":"global-m","permissionMode":"default","dreamEnabled":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".drover", "settings.json"), []byte(`{"model":"proj-m","dreamEnabled":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".drover", "settings.local.json"), []byte(`{"model":"local-m","env":{"FOO":"bar"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(proj)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := l.Get()
	if s.Model != "local-m" {
		t.Fatalf("model: got %q want local override", s.Model)
	}
	if !s.DreamEnabled {
		t.Fatal("dreamEnabled should stay true from project merge")
	}
	if s.Env["FOO"] != "bar" {
		t.Fatalf("env: %v", s.Env)
	}
}

func TestLoader_LegacyClaudePathsStillLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".claude", "settings.json"), []byte(`{"model":"legacy-m"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(proj)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := l.Get().Model; got != "legacy-m" {
		t.Fatalf("model: got %q want legacy-m", got)
	}
}

func TestLoader_DroverOverridesLegacyClaudeAtSameTier(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".claude", "settings.json"), []byte(`{"model":"legacy-m"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".drover", "settings.json"), []byte(`{"model":"drover-m"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(proj)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := l.Get().Model; got != "drover-m" {
		t.Fatalf("model: got %q want drover-m", got)
	}
}

func TestLoader_DreamRetentionMerge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".drover", "settings.json"), []byte(`{"dreamMaxRetentionEntries":1000}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".drover", "settings.json"), []byte(`{"dreamMaxRetentionEntries":40,"dreamMaxRetentionAgeDays":90}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLoader(proj)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := l.Get()
	if s.DreamMaxRetentionEntries != 40 || s.DreamMaxRetentionAgeDays != 90 {
		t.Fatalf("retention: entries=%d age=%d", s.DreamMaxRetentionEntries, s.DreamMaxRetentionAgeDays)
	}
}

func TestLoader_ProjectMarkdownIgnoreGlobs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := filepath.Join(home, "app")
	sub := filepath.Join(proj, "keep", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	skipDir := filepath.Join(proj, "vendor", "lib")
	if err := os.MkdirAll(skipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skipDir, "CLAUDE.md"), []byte("skip me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".drover", "settings.json"), []byte(`{"projectMarkdownIgnoreGlobs":["vendor/**"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(sub)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	inj := l.SystemInjection()
	if !containsAll(inj, "keep me", "root") {
		t.Fatalf("expected kept paths in injection:\n%s", inj)
	}
	if strings.Contains(inj, "skip me") {
		t.Fatalf("vendor CLAUDE.md should be ignored:\n%s", inj)
	}
}

func TestLoader_CLAUDEmdWalk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := filepath.Join(home, "monorepo")
	sub := filepath.Join(proj, "pkg", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte("api rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte("root rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(sub)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	inj := l.SystemInjection()
	if inj == "" {
		t.Fatal("expected system injection")
	}
	// Walk collects from global (none), then upward: pkg/api, then monorepo root.
	if !containsAll(inj, "api rules", "root rules") {
		t.Fatalf("injection missing content:\n%s", inj)
	}
}

func TestLoader_ProjectMarkdownByteCap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(proj, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".drover", "settings.json"), []byte(`{"projectMarkdownMaxBytes":800}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte(strings.Repeat("x", 5000)), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(proj)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	inj := l.SystemInjection()
	if len(inj) > 1200 {
		t.Fatalf("expected capped injection, got len %d", len(inj))
	}
	if !strings.Contains(inj, "truncated") {
		t.Fatalf("missing truncation marker:\n%s", inj)
	}
}

func TestLoader_Save_MergesProjectFile(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".drover", "settings.json"), []byte(`{"model":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(proj)
	if err := l.Load(); err != nil {
		t.Fatal(err)
	}
	if err := l.Save(Settings{MaxTokens: 4096}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".drover", "settings.json")); err != nil {
		t.Fatalf("Save should write .drover/settings.json: %v", err)
	}
	if err := l.Load(); err != nil {
		t.Fatal(err)
	}
	s := l.Get()
	if s.Model != "old" || s.MaxTokens != 4096 {
		t.Fatalf("after Save: %+v", s)
	}
}

func TestEffectiveDisableAutoCompaction(t *testing.T) {
	t.Setenv("DROVER_CODE_DISABLE_AUTO_COMPACTION", "")
	if EffectiveDisableAutoCompaction(Settings{}) {
		t.Fatal("empty")
	}
	if !EffectiveDisableAutoCompaction(Settings{DisableAutoCompaction: true}) {
		t.Fatal("settings")
	}
	t.Setenv("DROVER_CODE_DISABLE_AUTO_COMPACTION", "1")
	if !EffectiveDisableAutoCompaction(Settings{}) {
		t.Fatal("env")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
