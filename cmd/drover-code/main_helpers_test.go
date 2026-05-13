package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/coordinator"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/tools"
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

func TestResolveUndercoverMode(t *testing.T) {
	bTrue := true
	bFalse := false
	
	if !resolveUndercoverMode(config.Settings{UndercoverMode: &bTrue}, "") {
		t.Fatal("expected true when settings.UndercoverMode is true")
	}
	if resolveUndercoverMode(config.Settings{UndercoverMode: &bFalse}, "") {
		t.Fatal("expected false when settings.UndercoverMode is false")
	}
	
	// When nil, it should fallback to detection. We can test detection loosely.
	// An empty dir won't have .cline rules.
	if resolveUndercoverMode(config.Settings{}, t.TempDir()) {
		t.Fatal("expected false for empty temp dir fallback")
	}
}

func TestHeadlessUseJSONL(t *testing.T) {
	t.Setenv("DROVER_CODE_HEADLESS_PLAIN", "1")
	if headlessUseJSONL() {
		t.Fatal("expected false when HEADLESS_PLAIN is 1")
	}
	
	t.Setenv("DROVER_CODE_HEADLESS_PLAIN", "")
	t.Setenv("DROVER_CODE_JSONL", "1")
	if !headlessUseJSONL() {
		t.Fatal("expected true when JSONL is 1")
	}
	t.Setenv("DROVER_CODE_JSONL", "")
}

func TestBuildPermEngine(t *testing.T) {
	// Test the fallback mechanism and settings precedence
	t.Setenv("DROVER_CODE_PERMISSION_PRESET", permissions.PresetUnikernel)
	e := buildPermEngine(config.Settings{}, t.TempDir(), func(ctx context.Context, req tools.PermissionRequest) tools.Decision { return 0 })
	if e == nil {
		t.Fatal("expected permission engine")
	}
	t.Setenv("DROVER_CODE_PERMISSION_PRESET", "")
}

func TestFormatCoordinatorDreamTurn(t *testing.T) {
	out := coordinator.ExecuteOutcome{
		Summary: "Did some work.",
	}
	res := formatCoordinatorDreamTurn(out)
	if res != "Did some work." {
		t.Fatalf("expected summary only, got %q", res)
	}

	out.Workers = []coordinator.WorkerResult{
		{Index: 0, Task: "Task 1", Output: "chunk1", IsError: false},
		{Index: 1, Task: "Task 2", Output: strings.Repeat("a", 6010), IsError: true},
	}
	res = formatCoordinatorDreamTurn(out)
	if !strings.Contains(res, "Did some work.") {
		t.Error("missing summary")
	}
	if !strings.Contains(res, "Worker 1 (Task 1) [ok]") {
		t.Error("missing worker 1")
	}
	if !strings.Contains(res, "Worker 2 (Task 2) [error]") {
		t.Error("missing worker 2")
	}
	if !strings.Contains(res, "…") {
		t.Error("expected truncation")
	}
}

func TestFlushDreamWorker(t *testing.T) {
	// Should handle nil gracefully
	flushDreamWorker(nil, "s", nil)

	// We can't fully test flush without a mock dream.Worker but we can test
	// that nil works without panicking.
}

func TestSetupDream(t *testing.T) {
	// Test when disabled
	store, w := setupDream(config.Settings{DreamEnabled: false}, t.TempDir(), nil)
	if store != nil || w != nil {
		t.Fatal("expected nil store and worker when disabled")
	}

	// Test when enabled but empty dir
	dir := t.TempDir()
	store, w = setupDream(config.Settings{DreamEnabled: true}, dir, nil)
	if store == nil || w == nil {
		t.Fatal("expected store and worker")
	}
}

func TestPrintEvents(t *testing.T) {
	ch := make(chan agent.Event, 10)
	ch <- agent.TextDeltaEvent{Text: "delta"}
	ch <- agent.ToolStartEvent{InputSummary: "start"}
	ch <- agent.ToolDoneEvent{IsError: true, OutputSummary: "err done"}
	ch <- agent.ToolDoneEvent{IsError: false, OutputSummary: "ok done"}
	ch <- agent.DoneEvent{}
	ch <- agent.ErrorEvent{Err: errors.New("err")}
	ch <- agent.HeartbeatEvent{Turn: 1}
	ch <- agent.CompactionStartEvent{Round: 1, MaxRounds: 3, EstimatedTokensBefore: 100}
	ch <- agent.CompactionDoneEvent{Round: 1, EstimatedTokensAfter: 50, Err: nil}
	ch <- agent.CompactionDoneEvent{Round: 2, Err: errors.New("fail")}
	close(ch)
	
	printEvents(ch)
	// Since this prints to stdout/stderr, we just ensure it doesn't panic.
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := loadConfig(dir)
	if cfg == nil {
		t.Fatal("expected config loader")
	}
}

func TestHeadlessPermissionEngine(t *testing.T) {
	// Test default (bypass)
	t.Setenv("DROVER_CODE_PERMISSION_PRESET", "")
	e1 := headlessPermissionEngine(config.Settings{}, t.TempDir())
	if e1 == nil {
		t.Fatal("expected engine")
	}

	// Test unikernel preset
	t.Setenv("DROVER_CODE_PERMISSION_PRESET", permissions.PresetUnikernel)
	e2 := headlessPermissionEngine(config.Settings{}, t.TempDir())
	if e2 == nil {
		t.Fatal("expected engine")
	}
	t.Setenv("DROVER_CODE_PERMISSION_PRESET", "")
}
