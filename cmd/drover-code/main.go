package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/bridge"
	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/coordinator"
	"github.com/cloudshuttle/drover-code/internal/dream"
	github "github.com/cloudshuttle/drover-code/internal/github"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/tui"
	"github.com/cloudshuttle/drover-code/internal/undercover"
)

const defaultModel = "claude-haiku-4-5-20251001"

// startupFlags is set for all modes except `webhook` (parsed after dispatch).
var startupFlags cliFlags

func main() {
	// Subcommand dispatch: `drover-code webhook` starts the webhook server.
	if len(os.Args) > 1 && os.Args[1] == "webhook" {
		runWebhookServer()
		return
	}

	var err error
	startupFlags, err = parseCLIFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "drover-code:", err)
		os.Exit(2)
	}

	apiKey := requireAnthropicAPIKey()
	workDir := mustGetwd()

	cfg := loadConfig(workDir)
	settings := cfg.Get()
	if startupFlags.CoordinatorRemote {
		settings.CoordinatorRemote = true
	}

	modelStr := coalesce(os.Getenv("ANTHROPIC_MODEL"), settings.Model, defaultModel)

	undercoverActive := resolveUndercoverMode(settings, workDir)
	sysPrompt := buildSystemPrompt(workDir, cfg.SystemInjection(), undercoverActive)

	client := api.NewClient(apiKey, modelStr)
	if u := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); u != "" {
		client.SetBaseURL(u)
	}
	mgr := convo.NewManagerWithSystem(sysPrompt)
	config.ApplyConvoHeuristics(mgr, settings)
	registry := tools.NewRegistry()
	tools.RegisterAll(registry, workDir)

	dreamStore, dreamWorker := setupDream(settings, workDir, client)

	lf := telemetry.New(telemetry.ConfigFromEnv())
	defer lf.Flush()

	ctx, cancel := signal.NotifyContext(telemetry.WithTracer(context.Background(), lf), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Headless must win over IDE bridge and coordinator when requested; otherwise
	// a shell profile or project settings can accidentally force the wrong mode.
	switch {
	case wantsHeadlessMode():
		runHeadless(ctx, client, mgr, registry, settings, workDir, dreamWorker)

	case envTruthy("DROVER_CODE_IDE_BRIDGE"):
		runBridgeMode(ctx, client, mgr, registry, settings, workDir, dreamWorker)

	case envTruthy("DROVER_CODE_COORDINATOR_MODE") || settings.CoordinatorMode || settings.CoordinatorRemote:
		runCoordinatorMode(ctx, client, registry, modelStr, workDir, settings, dreamWorker)

	default:
		runTUI(ctx, client, mgr, registry, modelStr, settings, workDir, dreamWorker, dreamStore)
	}
}

// ── Webhook server ─────────────────────────────────────────────────────────

func runWebhookServer() {
	apiKey := requireAnthropicAPIKey()
	ghToken := requireEnv("GITHUB_TOKEN")
	webhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	addr := coalesce(os.Getenv("WEBHOOK_ADDR"), ":8080")
	workBase := coalesce(os.Getenv("WEBHOOK_WORK_DIR"), "/tmp/drover-code-work")

	if err := os.MkdirAll(workBase, 0o755); err != nil {
		log.Fatalf("create work dir: %v", err)
	}

	client := api.NewClient(apiKey, defaultModel)
	if u := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); u != "" {
		client.SetBaseURL(u)
	}
	ghClient := github.NewClient(ghToken)
	runner := github.NewRunner(ghClient, client, workBase)
	srv := github.NewServer(runner, webhookSecret)
	httpSrv := srv.HTTPServer(addr)

	log.Printf("starting drover-code webhook server on %s", addr)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("webhook server: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	signal.Stop(sigCh)

	shCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shCtx); err != nil {
		log.Printf("webhook server shutdown: %v", err)
	}
}

// ── TUI mode ─────────────────────────────────────────────────────────────────

func runTUI(
	ctx context.Context,
	client *api.Client,
	mgr *convo.Manager,
	registry *tools.Registry,
	modelStr string,
	settings config.Settings,
	workDir string,
	dw *dream.Worker,
	ds dream.Store,
) {
	if ds != nil {
		if inj := dream.BuildInjection(ds, 5); inj != "" {
			mgr.SetSystemPrompt(mgr.SystemPrompt() + "\n\n" + inj)
		}
	}
	if dw != nil {
		dw.Start(ctx)
	}

	prog := tui.NewProgram(ctx, client, mgr, registry, modelStr, settings, workDir)
	if err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	flushDreamWorker(dw, "tui", mgr.Messages())
}

// ── Headless mode ─────────────────────────────────────────────────────────────
//
// Activation (any of): DROVER_CODE_HEADLESS (1/true/yes/on), --headless,
// --prompt / --prompt-file, DROVER_CODE_PERMISSION_PRESET=unikernel,
// or stdin not a TTY (legacy). Prefer env or flag in automation — TTY detection alone is fragile
// under systemd, SSH, and VM consoles. See design/11-headless-orchestration.md.
//
// Stdin protocol: one user message per non-empty line; empty lines and "/quit"
// are ignored. Permissions use bypass + AllowAll (no interactive prompts).
//
// Exit codes (target contract; see design doc § Headless contract):
//
//	0 — success
//	1 — agent/task failure
//	2 — usage/config (flag parse, missing env, prompt-file IO, stdin IO)
//	3 — permission/policy (reserved)
//	4 — timeout (context deadline exceeded)
//	5 — transient infra (heuristic: network timeout, rate limit, connection errors)
//
// Output: JSON Lines on stdout when stdout is not a TTY (piped / automation), or when
// DROVER_CODE_JSONL=1. When stdout is a TTY (interactive terminal), plain streaming
// matches pre–Phase-2 behavior unless DROVER_CODE_JSONL=1. DROVER_CODE_HEADLESS_PLAIN=1
// always forces plain; it is redundant on a TTY but keeps scripts explicit.
//
// Input precedence: --prompt / -p, then --prompt-file, then stdin lines.
//
// DROVER_CODE_PERMISSION_PRESET: unset / "bypass" / "default" → bypass mode (legacy).
// "unikernel" → allowlist from MergeUnikernelPreset + settings; git_push denied by default;
// rules file is not loaded (no widening via permissions.json). Unknown preset → exit 2.
//
// Completion artifact: --result-json path or DROVER_CODE_RESULT_PATH — atomic JSON (schema_version 1).
// Optional: DROVER_CODE_POST_HOOK_OWNER, DROVER_CODE_PR_URL, DROVER_CODE_TESTS_PASSED.
//
// Phase 5: DROVER_CODE_TIMEOUT_SECS (wall-clock, child of signal ctx) → exit 4 on deadline.
// DROVER_CODE_MAX_TOKENS caps cumulative assistant output tokens only (not context/input size) → exit 4.

func headlessPermissionEngine(settings config.Settings, workDir string) *permissions.Engine {
	rulesPath := filepath.Join(workDir, ".claude", "permissions.json")
	preset := strings.ToLower(strings.TrimSpace(os.Getenv("DROVER_CODE_PERMISSION_PRESET")))
	switch preset {
	case "", "bypass", "default":
		return permissions.NewEngine(
			permissions.ModeBypass,
			settings.AllowedTools,
			settings.DeniedTools,
			rulesPath,
			tools.AllowAll,
		)
	case permissions.PresetUnikernel:
		allow, deny := permissions.MergeUnikernelPreset(settings.AllowedTools, settings.DeniedTools)
		return permissions.NewEngine(
			permissions.ModeAllowlist,
			allow,
			deny,
			"",
			tools.AllowAll,
		)
	default:
		fmt.Fprintf(os.Stderr, "drover-code: unknown DROVER_CODE_PERMISSION_PRESET=%q (use %q or leave unset)\n",
			preset, permissions.PresetUnikernel)
		os.Exit(2)
		return nil
	}
}

func runHeadless(
	ctx context.Context,
	client *api.Client,
	mgr *convo.Manager,
	registry *tools.Registry,
	settings config.Settings,
	workDir string,
	dw *dream.Worker,
) {
	ctx, cancelTimeout := headlessTimeout(ctx)
	defer cancelTimeout()

	eventCh := make(chan agent.Event, 256)
	eng := headlessPermissionEngine(settings, workDir)
	loop := agent.NewLoop(client, mgr, registry, eng, eventCh)
	config.ApplyAgentLoopOptions(loop, settings)
	if n := headlessMaxSessionTokens(); n > 0 {
		loop.SetMaxSessionTokens(n)
	}
	useJSONL := headlessUseJSONL()
	if useJSONL {
		go drainHeadlessJSONL(eventCh)
	} else {
		go printEvents(eventCh)
	}

	if dw != nil {
		dw.Start(ctx)
	}

	hadErr := false
	exitCode := 1
	var sumTurns int
	var lastAgentErr error

	runOne := func(input string) bool {
		input = strings.TrimSpace(input)
		if input == "" || input == "/quit" {
			return false
		}
		err := loop.Run(ctx, input)
		sumTurns += loop.LastRunTurns()
		if err != nil {
			hadErr = true
			lastAgentErr = err
			exitCode = headlessExitCode(err)
			if useJSONL {
				fmt.Fprintf(os.Stderr, "drover-code: agent error: %v\n", err)
			}
			return true
		}
		return false
	}

	switch {
	case startupFlags.Prompt != "":
		if stop := runOne(startupFlags.Prompt); stop {
			break
		}
	case startupFlags.PromptFile != "":
		b, err := os.ReadFile(startupFlags.PromptFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "headless: read --prompt-file: %v\n", err)
			close(eventCh)
			if dw != nil {
				dw.Wait()
			}
			writeHeadlessResultFile(workDir, loop, false, sumTurns, err.Error())
			os.Exit(2)
		}
		if stop := runOne(string(b)); stop {
			break
		}
	default:
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if stop := runOne(scanner.Text()); stop {
				break
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "headless: stdin: %v\n", err)
			close(eventCh)
			if dw != nil {
				dw.Wait()
			}
			writeHeadlessResultFile(workDir, loop, false, sumTurns, err.Error())
			os.Exit(2)
		}
	}

	close(eventCh)

	flushDreamWorker(dw, "headless", mgr.Messages())

	overallOK := !hadErr && ctx.Err() == nil
	errMsg := ""
	if lastAgentErr != nil {
		errMsg = lastAgentErr.Error()
	}
	writeHeadlessResultFile(workDir, loop, overallOK, sumTurns, errMsg)

	if hadErr {
		os.Exit(exitCode)
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			os.Exit(4)
		}
		os.Exit(1)
	}
}

func headlessExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, agent.ErrTokenBudgetExceeded) {
		return 4
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return 4
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return 5
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection refused") {
		return 5
	}
	return 1
}

// ── Coordinator mode ──────────────────────────────────────────────────────────

func runCoordinatorMode(
	ctx context.Context,
	client *api.Client,
	registry *tools.Registry,
	modelStr string,
	workDir string,
	settings config.Settings,
	dw *dream.Worker,
) {
	_ = modelStr
	fmt.Fprintln(os.Stderr, "coordinator mode — spawning parallel worker agents")
	eventCh := make(chan agent.Event, 512)
	coord := coordinator.New(client, registry, workDir, eventCh, settings)
	go printEvents(eventCh)

	var coordMgr *convo.Manager
	if dw != nil {
		coordMgr = convo.NewManager()
		dw.Start(ctx)
		defer func() { flushDreamWorker(dw, "coordinator", coordMgr.Messages()) }()
	}

	scanner := bufio.NewScanner(os.Stdin)
	isInteractive := func() bool {
		stat, _ := os.Stdin.Stat()
		return (stat.Mode() & os.ModeCharDevice) != 0
	}()

	prompt := func() {
		if isInteractive {
			fmt.Print("> ")
		}
	}
	prompt()
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			prompt()
			continue
		}
		if strings.ToLower(input) == "/quit" || strings.ToLower(input) == "exit" {
			break
		}
		out, err := coord.ExecuteWithResults(ctx, input)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "coordinator error:", err)
			if coordMgr != nil {
				coordMgr.Append(api.UserMessage(input))
				coordMgr.Append(api.UserMessage("Coordinator error: " + err.Error()))
			}
		} else {
			fmt.Println(out.Summary)
			if coordMgr != nil {
				coordMgr.Append(api.UserMessage(input))
				text := formatCoordinatorDreamTurn(out)
				coordMgr.Append(api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: text}}))
			}
		}
		fmt.Println()
		prompt()
	}
}

// ── IDE bridge mode ───────────────────────────────────────────────────────────

func runBridgeMode(
	ctx context.Context,
	client *api.Client,
	mgr *convo.Manager,
	registry *tools.Registry,
	settings config.Settings,
	workDir string,
	dw *dream.Worker,
) {
	_ = workDir
	eventCh := make(chan agent.Event, 256)
	eng := permissions.NewEngine(
		permissions.ModeBypass,
		settings.AllowedTools,
		settings.DeniedTools,
		filepath.Join(workDir, ".claude", "permissions.json"),
		tools.AllowAll,
	)
	loop := agent.NewLoop(client, mgr, registry, eng, eventCh)
	config.ApplyAgentLoopOptions(loop, settings)
	go func() {
		for range eventCh {
		}
	}()

	if dw != nil {
		dw.Start(ctx)
		defer func() { flushDreamWorker(dw, "bridge", mgr.Messages()) }()
	}

	b := bridge.NewStdioBridge()
	bridge.RegisterStandardHandlers(b, func(bCtx context.Context, input string) (string, error) {
		innerCh := make(chan agent.Event, 256)
		innerLoop := agent.NewLoop(client, mgr, registry, eng, innerCh)
		config.ApplyAgentLoopOptions(innerLoop, settings)
		var out strings.Builder
		done := make(chan struct{})
		go func() {
			defer close(done)
			for ev := range innerCh {
				if td, ok := ev.(agent.TextDeltaEvent); ok {
					out.WriteString(td.Text)
				}
			}
		}()
		err := innerLoop.Run(bCtx, input)
		close(innerCh)
		<-done
		_ = loop // keep outer loop alive
		return out.String(), err
	})

	if err := b.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, "bridge error:", err)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func loadConfig(workDir string) *config.Loader {
	cfg := config.NewLoader(workDir)
	if err := cfg.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config: %v\n", err)
	}
	return cfg
}

func setupDream(settings config.Settings, workDir string, client *api.Client) (dream.Store, *dream.Worker) {
	if !settings.DreamEnabled {
		return nil, nil
	}
	store, err := dream.OpenStore(workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: dream store: %v\n", err)
		return nil, nil
	}
	ret := dream.Retention{
		MaxEntries: settings.DreamMaxRetentionEntries,
		MaxAgeDays: settings.DreamMaxRetentionAgeDays,
	}
	ret.ApplyEnvOverrides()
	if ret.Active() {
		_ = store.Prune(ret)
	}
	return store, dream.NewWorker(store, client, ret)
}

const maxCoordinatorWorkerRunesForDream = 6000

func formatCoordinatorDreamTurn(out coordinator.ExecuteOutcome) string {
	if len(out.Workers) == 0 {
		return out.Summary
	}
	var b strings.Builder
	b.WriteString(out.Summary)
	b.WriteString("\n\n---\nParallel workers:\n")
	for _, w := range out.Workers {
		status := "ok"
		if w.IsError {
			status = "error"
		}
		fmt.Fprintf(&b, "\n### Worker %d (%s) [%s]\n", w.Index+1, w.Task, status)
		chunk := w.Output
		if utf8.RuneCountInString(chunk) > maxCoordinatorWorkerRunesForDream {
			r := []rune(chunk)
			chunk = string(r[:maxCoordinatorWorkerRunesForDream-1]) + "…"
		}
		b.WriteString(chunk)
		b.WriteByte('\n')
	}
	return b.String()
}

func flushDreamWorker(dw *dream.Worker, sessionID string, msgs []api.Message) {
	if dw == nil {
		return
	}
	dw.Trigger(dream.Session{ID: sessionID, Messages: msgs})
	dw.Wait()
}

func resolveUndercoverMode(settings config.Settings, workDir string) bool {
	if settings.UndercoverMode != nil {
		return *settings.UndercoverMode
	}
	return undercover.Detect(workDir).Active
}

func buildPermEngine(settings config.Settings, workDir string, fallback tools.PermissionFunc) *permissions.Engine {
	return permissions.NewEngine(
		permissions.ParseMode(settings.PermissionMode),
		settings.AllowedTools,
		settings.DeniedTools,
		filepath.Join(workDir, ".claude", "permissions.json"),
		fallback,
	)
}

func buildSystemPrompt(workDir, projectMarkdown string, undercoverActive bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are drover-code, an agentic coding assistant.\nWorking directory: %s\n\n", workDir)
	b.WriteString("Tools: read/write/edit files, bash, glob, grep, git, web fetch.\n")
	b.WriteString("Read files before editing. Use edit_file for targeted changes.\n")
	b.WriteString("Check git_status before staging or committing.\n")
	b.WriteString("When the user asks for a deliverable in the repository (spec, plan, ADR, checklist, or code), prefer creating or updating a file with write_file or edit_file under the working directory unless they clearly want chat-only output.\n")
	if undercoverActive {
		b.WriteString("\n\n")
		b.WriteString(undercover.SystemPromptFragment)
	}
	if projectMarkdown != "" {
		b.WriteString("\n\n")
		b.WriteString(projectMarkdown)
	}
	return b.String()
}

func printEvents(ch <-chan agent.Event) {
	for event := range ch {
		switch e := event.(type) {
		case agent.TextDeltaEvent:
			fmt.Print(e.Text)
		case agent.ToolStartEvent:
			fmt.Fprintf(os.Stderr, "\n⚙  %s\n", e.InputSummary)
		case agent.ToolDoneEvent:
			if e.IsError {
				fmt.Fprintf(os.Stderr, "✗  %s\n", e.OutputSummary)
			} else {
				fmt.Fprintf(os.Stderr, "✓  %s\n", e.OutputSummary)
			}
		case agent.DoneEvent:
			fmt.Print("\n")
		case agent.ErrorEvent:
			fmt.Fprintln(os.Stderr, "error:", e.Err)
		case agent.HeartbeatEvent:
			fmt.Fprintf(os.Stderr, "[heartbeat] turn=%d ts=%s\n", e.Turn, e.Time.Format(time.RFC3339Nano))
		case agent.CompactionStartEvent:
			fmt.Fprintf(os.Stderr, "[compaction] round %d/%d ~%d est. tokens before\n",
				e.Round, e.MaxRounds, e.EstimatedTokensBefore)
		case agent.CompactionDoneEvent:
			if e.Err != nil {
				fmt.Fprintf(os.Stderr, "[compaction] round %d failed after %v: %v\n", e.Round, e.Duration, e.Err)
			} else {
				fmt.Fprintf(os.Stderr, "[compaction] round %d done in %v ~%d tokens after\n",
					e.Round, e.Duration, e.EstimatedTokensAfter)
			}
		}
	}
}

// headlessUseJSONL chooses machine-oriented JSON Lines vs human plain streaming.
func headlessUseJSONL() bool {
	if envTruthy("DROVER_CODE_HEADLESS_PLAIN") {
		return false
	}
	if envTruthy("DROVER_CODE_JSONL") {
		return true
	}
	stat, err := os.Stdout.Stat()
	if err != nil {
		return true
	}
	// Pipe / redirect → JSONL; interactive terminal → plain text on stdout.
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// wantsHeadlessMode returns true when headless (non-TUI) execution is requested.
func wantsHeadlessMode() bool {
	if envTruthy("DROVER_CODE_HEADLESS") {
		return true
	}
	if startupFlags.Headless {
		return true
	}
	if startupFlags.Prompt != "" || startupFlags.PromptFile != "" {
		return true
	}
	// Unikernel preset only applies to headless; treat it as an explicit batch run.
	if p := strings.ToLower(strings.TrimSpace(os.Getenv("DROVER_CODE_PERMISSION_PRESET"))); p == permissions.PresetUnikernel {
		return true
	}
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "error: %s not set\n", key)
		os.Exit(2)
	}
	return v
}

func mustGetwd() string {
	d, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: getwd:", err)
		os.Exit(2)
	}
	return d
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
