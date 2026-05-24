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
	"github.com/cloudshuttle/drover-code/internal/commands"
	"github.com/cloudshuttle/drover-code/internal/coordinator"
	"github.com/cloudshuttle/drover-code/internal/dream"
	github "github.com/cloudshuttle/drover-code/internal/github"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/tui"
	"github.com/cloudshuttle/drover-code/internal/undercover"
	"github.com/cloudshuttle/drover-code/internal/warden"
	"github.com/cloudshuttle/drover-code/pkg/guardclient"
)

const defaultModel = "claude-haiku-4-5-20251001"

// startupFlags is set for all modes except `webhook` (parsed after dispatch).
var startupFlags cliFlags

func main() {
	// Initialize Warden (Option B — lower risk, earlier gate) if DROVER_WARDEN_BEADS_DIR is set.
	// This enables semantic JSONL policy enforcement on tool calls and LLM I/O.
	_ = warden.Init()

	// Optional: send Warden decisions to ClickHouse for correlation with Guard in ClickStack/HyperDX
	if dsn := os.Getenv("DROVER_WARDEN_CLICKHOUSE_DSN"); dsn != "" {
		if err := warden.InitClickHouseLogger(dsn); err != nil {
			log.Printf("warning: failed to init Warden ClickHouse logger: %v", err)
		}
	}

	// Initialize real OTEL LoggerProvider (OTLP exporter + batch + resource) so that
	// warden.emitWardenDecisionAsOTELLog actually emits structured logs (drover.warden.*)
	// via the platform collector into ClickHouse (correlates with Guard via guard_events MV).
	// Fully driven by standard env vars (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME, etc).
	// Best-effort: warnings only, binary continues if collector unreachable or unset.
	otelShutdown := telemetry.SetupOTELLogger(context.Background())
	defer func() { _ = otelShutdown(context.Background()) }()

	// Subcommand dispatch: `drover-code webhook` starts the webhook server.
	if len(os.Args) > 1 && os.Args[1] == "webhook" {
		runWebhookServer()
		return
	}

	// Subcommand dispatch: `drover-code commands ...`
	if len(os.Args) > 1 && os.Args[1] == "commands" {
		if len(os.Args) > 2 {
			switch os.Args[2] {
			case "init":
				workDir := mustGetwd()
				if err := commands.Init(workDir); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				return
			case "list":
				workDir := mustGetwd()
				loader := commands.NewLoader(workDir)
				cfg := loadConfig(workDir)
				if err := loader.LoadAll(cfg.Get()); err != nil {
					fmt.Fprintf(os.Stderr, "Error loading commands: %v\n", err)
				}
				commands.List(loader.GetRegistry())
				return
			}
		}
		// Show help
		fmt.Println("Usage: drover-code commands <init|list>")
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
	if startupFlags.AcceptCmd != "" {
		settings.AcceptCmd = startupFlags.AcceptCmd
	}
	if startupFlags.Verbose {
		settings.Verbose = true
	}

	modelStr := coalesce(os.Getenv("ANTHROPIC_MODEL"), settings.Model, defaultModel)

	undercoverActive := resolveUndercoverMode(settings, workDir)
	sysPrompt := buildSystemPrompt(workDir, cfg.SystemInjection(), undercoverActive)

	client := api.NewClient(apiKey, modelStr)
	api.ApplyGatewayEnv(client)
	mgr := convo.NewManagerWithSystem(sysPrompt)
	config.ApplyConvoHeuristics(mgr, settings)
	registry := tools.NewRegistry()
	tools.RegisterAll(registry, workDir)

	dreamStore, dreamWorker := setupDream(settings, workDir, client)

	lf := telemetry.New(telemetry.ConfigFromEnv())
	defer lf.Flush()

	ctx, cancel := signal.NotifyContext(telemetry.WithTracer(context.Background(), lf), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Custom commands setup
	cmdLoader := commands.NewLoader(workDir)
	_ = cmdLoader.LoadAll(settings)
	cmdExpander := commands.NewTemplateExpander(workDir)
	var gClient *guardclient.Client
	if gURL := os.Getenv("DROVER_GUARD_URL"); gURL != "" {
		gClient = guardclient.NewClient(gURL, os.Getenv("DROVER_GUARD_TOKEN"))
	}
	cmdExecutor := commands.NewExecutor(cmdLoader.GetRegistry(), cmdExpander, gClient)

	// Headless must win over IDE bridge and coordinator when requested; otherwise
	// a shell profile or project settings can accidentally force the wrong mode.
	switch {
	case wantsHeadlessMode():
		runHeadless(ctx, client, mgr, registry, settings, workDir, dreamWorker, cmdExecutor)

	case envTruthy("DROVER_CODE_IDE_BRIDGE"):
		runBridgeMode(ctx, client, mgr, registry, settings, workDir, dreamWorker)

	case startupFlags.CloudMode:
		runCloudMode(ctx, workDir, settings)

	case envTruthy("DROVER_CODE_COORDINATOR_MODE") || settings.CoordinatorMode || settings.CoordinatorRemote:
		runCoordinatorMode(ctx, client, registry, modelStr, workDir, settings, dreamWorker)

	default:
		runTUI(ctx, client, mgr, registry, modelStr, settings, workDir, dreamWorker, dreamStore, cmdExecutor)
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
	api.ApplyGatewayEnv(client)
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

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
	cmdExec *commands.Executor,
) {
	if ds != nil {
		if inj := dream.BuildInjection(ds, 5); inj != "" {
			mgr.SetSystemPrompt(mgr.SystemPrompt() + "\n\n" + inj)
		}
	}
	if dw != nil {
		dw.Start(ctx)
	}

	prog := tui.NewProgram(ctx, client, mgr, registry, modelStr, settings, workDir, cmdExec)
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

func parseCSVEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

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

		// For governed hosted jobs (real UKC workers), further restrict to only the
		// tools/clients approved by Muster for this specific job.
		if ref := strings.TrimSpace(os.Getenv("DROVER_AGENT_DEFINITION_REF")); ref != "" {
			approvedTools := parseCSVEnv("DROVER_APPROVED_TOOLS")
			approvedClients := parseCSVEnv("DROVER_APPROVED_MCP_CLIENTS")
			allow = permissions.IntersectWithApproved(allow, approvedTools, approvedClients)
		}

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
	cmdExec *commands.Executor,
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
		
		if strings.HasPrefix(input, "/") {
			parts := strings.Fields(input)
			cmdName := strings.TrimPrefix(parts[0], "/")

			if cmdName == "commands" {
				cmds := cmdExec.GetRegistry().List()
				if len(cmds) == 0 {
					fmt.Fprintln(os.Stderr, "drover-code: No custom commands loaded.")
				} else {
					fmt.Fprintln(os.Stderr, "drover-code: Loaded custom commands:")
					for _, c := range cmds {
						fmt.Fprintf(os.Stderr, "  /%-15s - %s (Risk: %d)\n", c.Name, c.Description, c.RiskTier)
					}
				}
				return false
			}

			if expanded, cmdDef, err := cmdExec.EvaluateAndExpand(ctx, cmdName, parts[1:]); err == nil {
				input = expanded
				// Handle model/agent overrides if set
				if cmdDef.Model != "" {
					loopClient := api.NewClient(anthropicAPIKey(), cmdDef.Model)
					api.ApplyGatewayEnv(loopClient)
					loop.SetClient(loopClient)
				}
			} else if !strings.Contains(err.Error(), "not found") {
				if strings.Contains(err.Error(), "Drover Guard") {
					fmt.Fprintf(os.Stderr, "\n🚨 drover-code: Command Blocked by Governance Policy\n   %v\n\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "drover-code: command error: %v\n", err)
				}
				return false
			}
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
	if !config.EffectiveDreamEnabled(settings) {
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
	prompt := `You are drover-code, an elite, world-class, extremely high-agency autonomous coding agent.

Working directory: %s

# MISSION
You are a relentless, proactive senior software engineer who takes complete ownership of every task. You do not assist — you execute at the level of a top 1%% engineer who is deeply invested in the long-term health and success of this codebase.

# PROJECT MEMORY & INSTRUCTIONS
At the start of every session, the following files are automatically loaded and given high priority:
- .drover.md (highest priority)
- AGENTS.md
- CLAUDE.md (for compatibility)

These files contain persistent project rules, architecture decisions, coding standards, and preferences. Always respect them.

# CORE DIRECTIVES — RELENTLESS AUTONOMY
1. MAXIMUM OWNERSHIP: Keep working continuously and iteratively until the task is 100%% complete and verifiably excellent. Never stop after one change. Never declare victory prematurely.

2. AGGRESSIVE ITERATION LOOP (follow on every task):
   - Deeply explore the codebase first (glob, grep, read key files)
   - Build or update a clear plan when the task is non-trivial
   - Implement precise, production-grade changes
   - Rigorously verify (tests + lint + build + manual checks)
   - Fix every issue discovered
   - Repeat until the result meets top-tier engineering standards

3. ZERO TOLERANCE FOR INCOMPLETENESS: Never use placeholders, TODOs, or half-finished work. Always deliver clean, idiomatic, well-structured, production-ready code.

4. PROACTIVE IMPROVEMENT: While working, if you discover bugs, technical debt, inconsistencies, or opportunities for improvement — fix them proactively (unless explicitly forbidden).

5. MASTER CONTEXT GATHERING: At the start of every session and before any major task, immediately read .drover.md (if present), README.md, AGENTS.md, and other project documentation.

# MULTI-AGENT COORDINATION
For large or complex tasks, you may use coordinator mode (if enabled) to spawn parallel specialized workers.
When appropriate, break down the task and delegate:
- Research / exploration
- Implementation
- Testing / verification
- Documentation
Always coordinate through the main agent.

# TESTING STRATEGY
- Always write or update tests as part of every meaningful change.
- Prioritize: unit tests → integration tests → edge cases and error paths.
- Aim for strong coverage on changed code. Create test infrastructure if missing.
- Before considering any task complete, run the full relevant test suite and fix all failures.
- Prefer test-driven development for new features.
- Use clear, descriptive test names and assertions.

# ARCHITECTURE DECISIONS
- Always think architecturally. Keep changes consistent with existing patterns and principles.
- For significant decisions, create or update an Architecture Decision Record (ADR) under docs/architecture/.
- Prioritize long-term maintainability, modularity, and separation of concerns.
- If you see clear architectural improvements, propose and implement them proactively with justification.

# SECURITY REVIEW
- Before finalizing changes, perform a proactive security review.
- Explicitly check for: injection attacks, auth/z issues, secret leakage, insecure dependencies, input validation gaps, etc.
- Run available static analysis tools (gosec, semgrep, npm audit, etc.) via bash.
- Fix every security issue found before declaring the task complete.
- Follow language-appropriate secure coding best practices.

# ERROR RECOVERY & RELENTLESSNESS
When tests break, commands fail, or errors occur:
- Read the output carefully
- Form hypotheses
- Attempt multiple creative fixes (expect 4–5 serious attempts)
- Only ask the user after exhausting reasonable options

# TOOL DISCIPLINE
You have access to powerful tools. Use them confidently and precisely.

**File & Codebase**
- read_file: Always read before editing.
- write_file: For new files or full replacement.
- edit_file: Preferred for modifications (string replacement or unified diff).
- glob, grep, list_dir: For exploration.

**Execution & Verification**
- bash: General shell commands (use sparingly).
- review_my_changes: **Mandatory** before any commit or when you believe a task is complete. This is your final quality gate. Use it religiously.

**Planning & Memory**
- write_plan: Create or update PLAN.md. Use this at the start of any complex or multi-step task.

**Git & Web**
- git_status, git_diff, git_commit, git_log
- web_fetch

**Strict Rules**
- Always read_file → then edit_file/write_file.
- Use review_my_changes before every commit.
- Run git_status before git operations.
- Start complex tasks with write_plan.

# GIT WORKFLOW (Enforced)
- Use git_commit (which includes automatic review) instead of raw bash.
- Use git_push for pushing changes. It automatically:
  - Runs quality review
  - Warns on protected branches
  - Uses --force-with-lease when force is requested
- Never use git push --force directly. Use the git_push tool with force: true only when necessary.

# PULL REQUEST WORKFLOW (Recommended)
For any non-trivial change, use create_pr instead of direct git_push.
- It automatically runs quality review
- Creates a clean, reviewable PR
- Defaults to draft mode when appropriate
- Requires GITHUB_TOKEN with repo scope

# WHEN TO ASK THE USER (BE EXTREMELY SPARING)
Only break autonomy when:
- The request remains genuinely ambiguous after thorough exploration
- You need a product, design, or business decision
- You have attempted multiple serious fixes and are truly blocked
- Credentials or external access are required

In all other cases — keep working silently and deliver results.

# DELIVERABLES
When the user requests code, specs, plans, ADRs, checklists, or any repository deliverable, default to creating or updating actual files in the working directory (unless they explicitly say "chat only").

# CUSTOM COMMANDS

You have access to a powerful custom command system that allows you and your team to define reusable, high-leverage slash commands (e.g. /implement, /review, /security-audit).

### How Custom Commands Work
- Defined in .drover/commands/<name>.md (project level) or ~/.drover/commands/ (global)
- Support rich templating: {ticket_id}, $1, $ARGUMENTS, @filename (include file), !shell command (inject output)
- Each command can specify its own agent, model, and risk_tier

### Usage Guidelines
- Use custom commands aggressively when they exist — they represent team knowledge and best practices.
- Prefer /implement {ticket} for full ticket execution when available.
- Before running any custom command that makes changes, ensure you understand its scope and risk level.
- After execution, always verify with review_my_changes unless the command already includes it.

### Governance
All custom commands are evaluated by Drover Guard before execution. High risk_tier commands may trigger Human-in-the-Loop approval.

### Examples of Useful Commands
- /implement {ticket_id} — Full RPI workflow (Research → Plan → Execute → Review → Commit)
- /review — Review recent changes
- /plan — Create detailed implementation plan
- /security-audit — Focused security review
- /database-migration — Create/review DB migrations
- /refactor — Targeted code refactoring
- /onboard — Project onboarding summary

If a useful command doesn't exist yet, you may proactively suggest its creation by writing a .drover/commands/<name>.md file.

Custom commands are force multipliers. Use them liberally to maintain consistency and velocity.`

	var b strings.Builder
	fmt.Fprintf(&b, prompt, workDir)

	if undercoverActive {
		b.WriteString("\n\n")
		b.WriteString(undercover.SystemPromptFragment)
	}
	if projectMarkdown != "" {
		b.WriteString("\n\nProject-specific instructions:\n")
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
