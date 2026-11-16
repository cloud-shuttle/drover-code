package planning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePlan_Execute(t *testing.T) {
	dir := t.TempDir()
	wp := &WritePlan{WorkDir: dir}

	if wp.Name() != "write_plan" {
		t.Errorf("unexpected name: %s", wp.Name())
	}
	if wp.NeedsPermission(nil) {
		t.Error("should not need permission")
	}

	// Bad JSON
	_, err := wp.Execute(context.Background(), []byte(`{bad json`))
	if err == nil {
		t.Error("expected error for bad json")
	}

	// Empty content
	_, err = wp.Execute(context.Background(), []byte(`{}`))
	if err == nil {
		t.Error("expected error for empty content")
	}

	// Success with default title
	out, err := wp.Execute(context.Background(), []byte(`{"content":"My plan"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "successfully") {
		t.Errorf("unexpected output: %s", out)
	}

	b, err := os.ReadFile(filepath.Join(dir, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# Current Task Plan") || !strings.Contains(string(b), "My plan") {
		t.Errorf("unexpected file content: %s", string(b))
	}

	// Success with custom title
	_, err = wp.Execute(context.Background(), []byte(`{"content":"My other plan","title":"Custom"}`))
	if err != nil {
		t.Fatal(err)
	}

	b, err = os.ReadFile(filepath.Join(dir, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# Custom") || !strings.Contains(string(b), "My other plan") {
		t.Errorf("unexpected file content: %s", string(b))
	}
	
	// Test description and schema exist
	if wp.Description() == "" {
		t.Error("missing description")
	}
	if len(wp.InputSchema()) == 0 {
		t.Error("missing schema")
	}
}
