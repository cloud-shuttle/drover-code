package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/undercover"
)

func TestHeadlessExitCode_mapping(t *testing.T) {
	if c := headlessExitCode(nil); c != 0 {
		t.Fatalf("nil -> %d", c)
	}
	if c := headlessExitCode(agent.ErrTokenBudgetExceeded); c != 4 {
		t.Fatalf("token budget -> %d", c)
	}
	if c := headlessExitCode(context.DeadlineExceeded); c != 4 {
		t.Fatalf("deadline -> %d", c)
	}
	var timeoutErr net.Error = errTimeout{}
	if c := headlessExitCode(timeoutErr); c != 5 {
		t.Fatalf("timeout -> %d", c)
	}
	if c := headlessExitCode(errors.New("HTTP 429 rate limited")); c != 5 {
		t.Fatalf("429 -> %d", c)
	}
	if c := headlessExitCode(errors.New("something else")); c != 1 {
		t.Fatalf("generic -> %d", c)
	}
}

type errTimeout struct{}

func (errTimeout) Error() string   { return "timeout" }
func (errTimeout) Timeout() bool   { return true }
func (errTimeout) Temporary() bool { return false }

func TestCoalesce(t *testing.T) {
	if got := coalesce("", "b", "c"); got != "b" {
		t.Fatalf("%q", got)
	}
	if got := coalesce("", "", ""); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestBuildSystemPrompt_undercoverAndProject(t *testing.T) {
	base := buildSystemPrompt("/proj", "", false)
	if !stringsContainsAll(base, []string{"/proj", "drover-code"}) {
		t.Fatalf("%s", base)
	}
	u := buildSystemPrompt("/p", "", true)
	if !strings.Contains(u, undercover.SystemPromptFragment[:40]) {
		t.Fatal("expected undercover fragment")
	}
	withDoc := buildSystemPrompt("/p", "## Title\n\nbody", false)
	if !strings.Contains(withDoc, "## Title") {
		t.Fatal(withDoc)
	}
}

func stringsContainsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestWantsHeadlessMode_envAndFlags(t *testing.T) {
	saved := startupFlags
	t.Cleanup(func() { startupFlags = saved })

	t.Setenv("DROVER_CODE_HEADLESS", "1")
	if !wantsHeadlessMode() {
		t.Fatal("DROVER_CODE_HEADLESS=1")
	}
	t.Setenv("DROVER_CODE_HEADLESS", "")
	startupFlags = cliFlags{Headless: true}
	if !wantsHeadlessMode() {
		t.Fatal("--headless")
	}
	startupFlags = cliFlags{Prompt: "x"}
	if !wantsHeadlessMode() {
		t.Fatal("--prompt")
	}
	t.Setenv("DROVER_CODE_PERMISSION_PRESET", permissions.PresetUnikernel)
	startupFlags = cliFlags{}
	if !wantsHeadlessMode() {
		t.Fatal("unikernel preset implies headless")
	}
	t.Setenv("DROVER_CODE_PERMISSION_PRESET", "")
}
