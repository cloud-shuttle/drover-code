package gopls

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDiagnostics_FiltersToEditedFile(t *testing.T) {
	const abs = "/mod/a.go"
	out := strings.Join([]string{
		"/mod/a.go:5:25-32: undefined: missing",
		"/mod/a.go:5:25-32: undefined: missing",
		"/mod/b.go:3:6-8: - other declaration of ok",
		"/mod/a.go:9:1-11:4: declared and not used: x",
		"noise",
	}, "\n")
	got := parseDiagnostics(out, abs)
	if len(got) != 2 {
		t.Fatalf("findings %+v", got)
	}
	if got[0].line != 5 || got[0].message != "undefined: missing" {
		t.Fatalf("first %+v", got[0])
	}
	if got[1].line != 9 || got[1].message != "declared and not used: x" {
		t.Fatalf("second %+v", got[1])
	}
	text := formatDiagnostics("a.go", got)
	if !strings.Contains(text, "a.go:5: undefined: missing") || strings.Contains(text, "b.go") {
		t.Fatalf("format %q", text)
	}
}

func TestFormatDiagnostics_Caps(t *testing.T) {
	found := make([]finding, maxFindings+2)
	for i := range found {
		found[i] = finding{line: i + 1, message: "m"}
	}
	text := formatDiagnostics("a.go", found)
	if !strings.Contains(text, "a.go:20: m") || strings.Contains(text, "a.go:21:") {
		t.Fatalf("cap failed:\n%s", text)
	}
	if !strings.Contains(text, "and 2 more") {
		t.Fatalf("missing extra count:\n%s", text)
	}
}

func TestAnnotate_SkipsNonGo(t *testing.T) {
	if got := Annotate(context.Background(), "a.txt", "/tmp/a.txt"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestAnnotate_ReportsUndefined(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	dir := t.TempDir()
	mod := filepath.Join(dir, "mod")
	if err := os.Mkdir(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "mod", "init", "example.com/diag")
	cmd.Dir = mod
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod init: %v\n%s", err, out)
	}
	a := filepath.Join(mod, "a.go")
	b := filepath.Join(mod, "b.go")
	if err := os.WriteFile(a, []byte("package diag\n\nfunc broken() int { return missing }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("package diag\n\nfunc other() int { return alsoMissing }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got := Annotate(ctx, "a.go", a)
	if !strings.Contains(got, "a.go:3: undefined: missing") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "b.go") || strings.Contains(got, "alsoMissing") {
		t.Fatalf("other file leaked: %q", got)
	}

	clean := filepath.Join(mod, "clean.go")
	if err := os.WriteFile(clean, []byte("package diag\n\nfunc ready() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// clean.go is a second declaration-free file, but the package still has errors.
	// The report for this file stays empty of the other files' messages.
	got = Annotate(ctx, "clean.go", clean)
	if strings.Contains(got, "a.go") || strings.Contains(got, "missing") {
		t.Fatalf("clean file report %q", got)
	}
}

func TestAnnotate_OutsideModule(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	dir := t.TempDir()
	abs := filepath.Join(dir, "a.go")
	if err := os.WriteFile(abs, []byte("package p\n\nfunc f() int { return missing }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Annotate(context.Background(), "a.go", abs)
	if !strings.Contains(got, "not in a Go module") {
		t.Fatalf("got %q", got)
	}
}
