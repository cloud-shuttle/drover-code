package permissions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/warden"
)

func TestEngine_BypassAlwaysAllows(t *testing.T) {
	e := NewEngine(ModeBypass, nil, nil, "", tools.AllowAll)
	d, err := e.Check(context.Background(), "bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if d != tools.Allow {
		t.Fatalf("expected Allow, got %v", d)
	}
}

func TestEngine_DenyBeatsAllow(t *testing.T) {
	promptCalled := false
	prompt := func(ctx context.Context, req tools.PermissionRequest) tools.Decision {
		promptCalled = true
		return tools.Allow
	}

	e := NewEngine(ModeDefault, []string{"bash"}, []string{"bash"}, "", prompt)
	d, err := e.Check(context.Background(), "bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if promptCalled {
		t.Fatalf("prompt should not be called when denied by config")
	}
	if d != tools.Deny {
		t.Fatalf("expected Deny, got %v", d)
	}
}

func TestEngine_AlwaysAllowPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "permissions.json")

	promptCalls := 0
	prompt := func(ctx context.Context, req tools.PermissionRequest) tools.Decision {
		promptCalls++
		return tools.AlwaysAllow
	}

	e := NewEngine(ModeDefault, nil, nil, rulesPath, prompt)
	d, err := e.Check(context.Background(), "bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if d != tools.AlwaysAllow {
		t.Fatalf("expected AlwaysAllow, got %v", d)
	}
	if promptCalls != 1 {
		t.Fatalf("expected promptCalls=1, got %d", promptCalls)
	}
	if _, err := os.Stat(rulesPath); err != nil {
		t.Fatalf("expected rules file written: %v", err)
	}

	// Reload with prompt that would fail if called.
	prompt2Called := false
	prompt2 := func(ctx context.Context, req tools.PermissionRequest) tools.Decision {
		prompt2Called = true
		return tools.Deny
	}
	e2 := NewEngine(ModeDefault, nil, nil, rulesPath, prompt2)
	d2, err := e2.Check(context.Background(), "bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if prompt2Called {
		t.Fatalf("prompt should not be called after persisted allow rule")
	}
	if d2 != tools.Allow {
		t.Fatalf("expected Allow due to persisted rule, got %v", d2)
	}
}

func TestEngine_ModeAllowlist(t *testing.T) {
	promptCalled := false
	prompt := func(ctx context.Context, req tools.PermissionRequest) tools.Decision {
		promptCalled = true
		return tools.Allow
	}

	allow, deny := MergeUnikernelPreset(nil, nil)
	e := NewEngine(ModeAllowlist, allow, deny, "", prompt)

	d, err := e.Check(context.Background(), "read_file", json.RawMessage(`{}`))
	if err != nil || d != tools.Allow {
		t.Fatalf("read_file: d=%v err=%v", d, err)
	}
	if promptCalled {
		t.Fatal("allowlist must not prompt")
	}

	d, err = e.Check(context.Background(), "git_push", json.RawMessage(`{}`))
	if err != nil || d != tools.Deny {
		t.Fatalf("git_push: d=%v err=%v", d, err)
	}

	d, err = e.Check(context.Background(), "unknown_tool", json.RawMessage(`{}`))
	if err != nil || d != tools.Deny {
		t.Fatalf("unknown_tool: d=%v err=%v", d, err)
	}
}

func TestEngine_FastDecisionAllowlist(t *testing.T) {
	allow, deny := MergeUnikernelPreset(nil, nil)
	e := NewEngine(ModeAllowlist, allow, deny, "", tools.AllowAll)
	d, ok := e.FastDecision("bash")
	if !ok || d != tools.Allow {
		t.Fatalf("bash: d=%v ok=%v", d, ok)
	}
	d, ok = e.FastDecision("git_push")
	if !ok || d != tools.Deny {
		t.Fatalf("git_push: d=%v ok=%v", d, ok)
	}
}

// TestEngine_WardenParticipates verifies that the new integration makes Warden
// decisions part of the unified Engine.Check / FastDecision (and thus permitFn).
// Uses a temp beads dir + ResetForTest to simulate an active semantic policy that
// blocks a tool that would otherwise be allowed by the preset/config.
func TestEngine_WardenParticipates(t *testing.T) {
	warden.ResetForTest()

	dir := t.TempDir()
	// Policy that will cause a block for "bash" via the dangerous_args path.
	pol := `{"id":"eng-001","type":"action","version":"1.0","scope":"mcp","rule":"test_engine_wd","tools":["bash"],"dangerous_args":["unit-test-block-bash"],"action":"block","severity":"critical"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "policies.jsonl"), []byte(pol), 0o644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("DROVER_WARDEN_BEADS_DIR", dir)
	defer os.Unsetenv("DROVER_WARDEN_BEADS_DIR")
	_ = warden.Init() // ensure loaded (the wrapper's Get will also trigger)

	// Unikernel-style allowlist that would normally permit "bash"
	allow, deny := MergeUnikernelPreset(nil, nil)
	e := NewEngine(ModeAllowlist, allow, deny, "", tools.AllowAll)

	// FastDecision (no input args) falls back to static allowlist for this arg-dependent bead policy.
	// (Warden still participates; arg-less Fast cannot see dangerous_args content.)
	d, ok := e.FastDecision("bash")
	if !ok || d != tools.Allow {
		t.Fatalf("FastDecision(bash) arg-less: want Allow (list),true got %v,%v", d, ok)
	}

	// Full Check path (with input) exercises Warden participation -> hard Deny even though list allows it.
	d, err := e.Check(context.Background(), "bash", json.RawMessage(`{"command":"unit-test-block-bash foo"}`))
	if err != nil || d != tools.Deny {
		t.Fatalf("Check(bash) with active blocking bead: want Deny, got %v err=%v", d, err)
	}

	// A non-blocked tool should still be allowed by the list + warden
	d, err = e.Check(context.Background(), "read_file", json.RawMessage(`{}`))
	if err != nil || d != tools.Allow {
		t.Fatalf("read_file should still be allowed: %v err=%v", d, err)
	}
}

