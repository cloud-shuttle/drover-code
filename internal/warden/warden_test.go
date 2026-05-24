package warden

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	droverwarden "github.com/cloud-shuttle/drover-warden/warden"
)

func TestWardenWrapper_DisabledReturnsAllow(t *testing.T) {
	resetForTest()
	os.Unsetenv("DROVER_WARDEN_BEADS_DIR")

	if err := Init(); err != nil {
		t.Fatalf("Init should not error when disabled: %v", err)
	}
	if Get() != nil {
		t.Fatal("expected nil Warden when disabled")
	}

	dec := CheckAction(context.Background(), &droverwarden.GuardRequest{
		ToolCall: &droverwarden.ToolCall{ToolName: "bash"},
	})
	if !dec.Allowed {
		t.Fatalf("disabled CheckAction should allow, got: %+v", dec)
	}
	if dec.Result.Reason != "warden disabled" {
		t.Fatalf("expected disabled reason, got %q", dec.Result.Reason)
	}

	// Also exercise the new helpers
	decI := CheckInput(context.Background(), &droverwarden.GuardRequest{Input: "test prompt"})
	if !decI.Allowed {
		t.Fatal("CheckInput disabled should allow")
	}

	decO := CheckOutput(context.Background(), &droverwarden.GuardRequest{Output: "some output"})
	if !decO.Allowed {
		t.Fatal("CheckOutput disabled should allow")
	}

	decT := CheckToolCall(context.Background(), "write_file", []byte(`{}`))
	if !decT.Allowed {
		t.Fatal("CheckToolCall disabled should allow")
	}
}

func TestWardenWrapper_ActiveWithTempBeads_BlocksOnPolicy(t *testing.T) {
	resetForTest()

	dir := t.TempDir()
	// Minimal policy that blocks any "bash" tool call (action type)
	// Policy that triggers the DangerousArgs path for tool "bash" (the warden action matcher
	// sets matched=true when a dangerous_arg is found in the "command" value).
	pol := `{"id":"t-001","type":"action","version":"1.0","scope":"mcp","rule":"test_block_bash","tools":["bash"],"dangerous_args":["rm -rf","dangerous-unit-test"],"action":"block","severity":"critical","description":"unit test bead"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "policies.jsonl"), []byte(pol), 0o644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("DROVER_WARDEN_BEADS_DIR", dir)
	defer os.Unsetenv("DROVER_WARDEN_BEADS_DIR")

	if err := Init(); err != nil {
		t.Fatalf("Init with valid beads dir failed: %v", err)
	}
	if Get() == nil {
		t.Fatal("expected active Warden after setting beads dir")
	}

	dec := CheckAction(context.Background(), &droverwarden.GuardRequest{
		TenantID: "test-tenant",
		ToolCall: &droverwarden.ToolCall{
			ToolName: "bash",
			Args:     map[string]any{"command": "dangerous-unit-test rm -rf /tmp"},
		},
	})
	if dec.Allowed {
		t.Fatalf("expected block from test bead policy, got allow: %+v", dec)
	}
	if dec.Result.Reason == "" {
		t.Fatal("expected a reason on block decision")
	}

	// CheckToolCall should also see the block (used by permissions.Engine)
	dec2 := CheckToolCall(context.Background(), "bash", []byte(`{"command":"dangerous-unit-test"}`))
	if dec2.Allowed {
		t.Fatal("CheckToolCall should also be blocked by the policy")
	}
}

func TestWardenWrapper_DefaultBeadsResolution(t *testing.T) {
	resetForTest()
	os.Unsetenv("DROVER_WARDEN_BEADS_DIR")

	// Create a temp tree that looks like a default layout the resolver understands
	root := t.TempDir()
	beadsDir := filepath.Join(root, "beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pol := `{"id":"def-001","type":"input","rule":"test_default","patterns":["unit-test-trigger"],"action":"block","severity":"low"}` + "\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "policies.jsonl"), []byte(pol), 0o644); err != nil {
		t.Fatal(err)
	}

	// Change cwd into the parent so relative "beads" candidate is found by resolver
	orig, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	if err := Init(); err != nil {
		t.Fatalf("Init should succeed with default beads layout: %v", err)
	}
	if Get() == nil {
		t.Fatal("expected Warden to activate via default beads resolution")
	}

	// A prompt containing the pattern should be blocked by the default bead
	dec := CheckInput(context.Background(), &droverwarden.GuardRequest{
		Input: "this is a unit-test-trigger prompt",
	})
	if dec.Allowed {
		t.Fatalf("expected block from default bead, got allow: %+v", dec)
	}
}