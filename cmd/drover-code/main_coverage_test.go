package main

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/tools"
)

func TestParseCSVEnv(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"read_file", []string{"read_file"}},
		{" read_file , ,bash,, mcp:github ", []string{"read_file", "bash", "mcp:github"}},
	}
	for _, c := range cases {
		t.Setenv("DROVER_TEST_CSV", c.raw)
		if got := parseCSVEnv("DROVER_TEST_CSV"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseCSVEnv(%q) = %#v, want %#v", c.raw, got, c.want)
		}
	}
}

// Governed hosted jobs (DROVER_AGENT_DEFINITION_REF set) must be restricted to
// the Muster-approved tools on top of the unikernel preset.
func TestHeadlessPermissionEngine_GovernedJobIntersectsApprovedTools(t *testing.T) {
	allow, _ := permissions.MergeUnikernelPreset(nil, nil)

	t.Setenv("DROVER_CODE_PERMISSION_PRESET", permissions.PresetUnikernel)
	t.Setenv("DROVER_AGENT_DEFINITION_REF", "")
	ungoverned := headlessPermissionEngine(config.Settings{}, t.TempDir())
	if ungoverned.Mode() != permissions.ModeAllowlist {
		t.Fatalf("unikernel preset mode = %v, want allowlist", ungoverned.Mode())
	}

	var approved, other string
	for _, name := range allow {
		if d, ok := ungoverned.FastDecision(name); ok && d == tools.Allow {
			if approved == "" {
				approved = name
			} else if other == "" {
				other = name
			}
		}
	}
	if approved == "" || other == "" {
		t.Skip("unikernel preset allows fewer than two tools; nothing to intersect")
	}

	t.Setenv("DROVER_AGENT_DEFINITION_REF", "agents/test@v1")
	t.Setenv("DROVER_APPROVED_TOOLS", approved)
	t.Setenv("DROVER_APPROVED_MCP_CLIENTS", "")
	governed := headlessPermissionEngine(config.Settings{}, t.TempDir())
	if d, ok := governed.FastDecision(approved); !ok || d != tools.Allow {
		t.Errorf("approved tool %q: decision=%v ok=%v, want allow", approved, d, ok)
	}
	if d, ok := governed.FastDecision(other); !ok || d != tools.Deny {
		t.Errorf("unapproved tool %q: decision=%v ok=%v, want deny", other, d, ok)
	}
}

func TestRequireEnvAndMustGetwd(t *testing.T) {
	t.Setenv("DROVER_TEST_REQUIRED", "value")
	if got := requireEnv("DROVER_TEST_REQUIRED"); got != "value" {
		t.Fatalf("requireEnv = %q", got)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := mustGetwd(); got != wd {
		t.Fatalf("mustGetwd = %q, want %q", got, wd)
	}
}

func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
}

// Blank lines are ignored and /quit or exit stop the coordinator REPL without
// dispatching any work (no LLM client is needed).
func TestRunCoordinatorMode_QuitCommands(t *testing.T) {
	for _, quit := range []string{"/quit", "EXIT"} {
		t.Run(quit, func(t *testing.T) {
			withStdin(t, "\n   \n"+quit+"\nnever reached\n")
			done := make(chan struct{})
			go func() {
				defer close(done)
				runCoordinatorMode(context.Background(), nil, tools.NewRegistry(), "", t.TempDir(), config.Settings{}, nil)
			}()
			<-done
		})
	}
}
