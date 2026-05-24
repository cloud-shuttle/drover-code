package quality

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReview_Execute(t *testing.T) {
	dir := t.TempDir()
	r := &Review{WorkDir: dir}

	if r.Name() != "review_my_changes" {
		t.Errorf("unexpected name")
	}
	if !r.NeedsPermission(nil) {
		t.Error("should need permission")
	}
	if len(r.InputSchema()) == 0 {
		t.Error("missing schema")
	}
	if r.Description() == "" {
		t.Error("missing description")
	}

	// No commands, no detected project
	out, err := r.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No project type detected") {
		t.Errorf("unexpected output: %s", out)
	}

	// Custom commands
	out, err = r.Execute(context.Background(), []byte(`{"commands":["echo hello"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "PASSED SUCCESSFULLY") {
		t.Errorf("unexpected output: %s", out)
	}

	// Custom command failure
	out, err = r.Execute(context.Background(), []byte(`{"commands":["false"]}`))
	if err != nil {
		t.Fatal("Execute should return nil err on command failure")
	}
	if !strings.Contains(out, "VERIFICATION FAILED") {
		t.Errorf("unexpected output: %s", out)
	}

	// Auto-detect go.mod
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module foo"), 0644); err != nil {
		t.Fatal(err)
	}
	cmds := r.autoDetectCommands()
	if len(cmds) < 3 || !strings.Contains(cmds[0], "go build") {
		t.Errorf("unexpected cmds: %v", cmds)
	}
}
