package harness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSmallWorkSkipPath(t *testing.T) {
	dir := t.TempDir()
	g := NewGate(dir)
	err := g.Accept(Spec{
		TaskID:     "t-small",
		WorkSize:   WorkSmall,
		SkipReason: "typo fix in README; reversible",
		Approver:   "operator:peter",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.AllowMutation("write_file", []string{"README.md"}); err != nil {
		t.Fatalf("small work should allow mutation: %v", err)
	}
}

func TestNonTrivialRequiresAcceptance(t *testing.T) {
	dir := t.TempDir()
	g := NewGate(dir)
	g.RequireForHeadless = true
	if err := g.AllowMutation("edit_file", nil); !errors.Is(err, ErrSpecRequired) {
		t.Fatalf("want ErrSpecRequired, got %v", err)
	}
}

func TestSpecHashMismatchBlocks(t *testing.T) {
	dir := t.TempDir()
	hash, err := WriteSPEC(dir, "t1", "# Goal\nship feature\n")
	if err != nil {
		t.Fatal(err)
	}
	g := NewGate(dir)
	if err := g.Accept(Spec{
		TaskID:      "t1",
		WorkSize:    WorkSingle,
		Goal:        "ship feature",
		Acceptance:  []string{"tests pass"},
		ContentHash: hash,
		Approver:    "operator:peter",
		Boundaries:  []string{"internal/harness"},
	}); err != nil {
		t.Fatal(err)
	}
	// Tamper SPEC
	if err := os.WriteFile(filepath.Join(dir, "docs", "tasks", "t1", "SPEC.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.AllowMutation("write_file", []string{"internal/harness/x.go"}); !errors.Is(err, ErrSpecMismatch) {
		t.Fatalf("want mismatch, got %v", err)
	}
}

func TestScopeExpansionBlocks(t *testing.T) {
	dir := t.TempDir()
	hash, err := WriteSPEC(dir, "t2", "goal")
	if err != nil {
		t.Fatal(err)
	}
	g := NewGate(dir)
	_ = g.Accept(Spec{
		TaskID: "t2", WorkSize: WorkSingle, Goal: "g", Acceptance: []string{"a"},
		ContentHash: hash, Approver: "op", Boundaries: []string{"internal/harness"},
	})
	if err := g.AllowMutation("write_file", []string{"internal/agent/loop.go"}); !errors.Is(err, ErrScopeExpanded) {
		t.Fatalf("want scope expand, got %v", err)
	}
}

func TestModelApprovedLabelInsufficient(t *testing.T) {
	g := NewGate(t.TempDir())
	err := g.Accept(Spec{TaskID: "x", WorkSize: WorkSmall, SkipReason: "x", Approver: ""})
	if err == nil {
		t.Fatal("empty approver must fail")
	}
}

func TestReadOnlyToolsBypass(t *testing.T) {
	g := NewGate(t.TempDir())
	if err := g.AllowMutation("read_file", nil); err != nil {
		t.Fatal(err)
	}
}
