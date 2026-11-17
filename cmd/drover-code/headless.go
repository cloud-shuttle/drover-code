package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloud-shuttle/drover-code/internal/agent"
	"github.com/cloud-shuttle/drover-code/internal/api"
	"github.com/cloud-shuttle/drover-code/internal/commands"
	"github.com/cloud-shuttle/drover-code/internal/config"
	"github.com/cloud-shuttle/drover-code/internal/convo"
	"github.com/cloud-shuttle/drover-code/internal/dream"
	"github.com/cloud-shuttle/drover-code/internal/permissions"
	"github.com/cloud-shuttle/drover-code/internal/session"
	"github.com/cloud-shuttle/drover-code/internal/telemetry"
	"github.com/cloud-shuttle/drover-code/internal/tools"
	shellpkg "github.com/cloud-shuttle/drover-code/internal/tools/shell"
)

// Headless mode.
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

func runHeadless(
	ctx context.Context,
	client *api.Client,
	mgr *convo.Manager,
	registry *tools.Registry,
	settings config.Settings,
	workDir string,
	dw *dream.Worker,
	cmdExec *commands.Executor,
	rec *session.Recorder,
) {
	ctx, cancelTimeout := headlessTimeout(ctx)
	defer cancelTimeout()

	eventCh := make(chan agent.Event, 256)
	eng := headlessPermissionEngine(settings, workDir)
	driver := agent.NewAnthropicInferenceDriver(client)
	executor := agent.NewDefaultToolExecutor(registry, eng, eventCh)
	loop := agent.NewLoop(driver, mgr, executor, registry, eventCh)
	var bashTool *shellpkg.Bash
	backgroundDone := make(chan shellpkg.Notice, 16)
	if b, ok := registry.Get("bash").(*shellpkg.Bash); ok && b.AllowBackground {
		bashTool = b
		b.SetOnDone(func(n shellpkg.Notice) {
			if n.Done {
				backgroundDone <- n
			}
		})
		stopBackground = b.StopAll
		defer func() { stopBackground = nil }()
		defer b.StopAll()
	}
	if rec != nil {
		loop.SetPersist(rec.Save)
		if len(mgr.Messages()) > 0 {
			fmt.Fprintf(os.Stderr, "drover-code: continuing %s\n", rec.ID())
		}
	}
	config.ApplyAgentLoopOptions(loop, settings)
	if n := headlessMaxSessionTokens(); n > 0 {
		loop.SetMaxSessionTokens(n)
	}
	useJSONL := headlessUseJSONL()
	headlessSession = sessionLogFromEnv()
	headlessSession.begin()
	if useJSONL {
		go drainHeadlessJSONL(eventCh)
	} else {
		go printEvents(eventCh)
	}

	if dw != nil {
		dw.Start(ctx)
	}

	// PHASE 5: Watchdog goroutine enforcing wall-clock timeout and token budget outside the agent loop.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		maxTokens := headlessMaxSessionTokens()
		for {
			select {
			case <-ctx.Done():
				if ctx.Err() == context.DeadlineExceeded {
					fmt.Fprintln(os.Stderr, "drover-code: watchdog: wall-clock timeout exceeded")
					writeHeadlessResultFile(workDir, loop, false, 0, "timeout exceeded (watchdog)")
					exitHeadless(4, "failed")
				}
				return // Normal exit or context cancellation
			case <-ticker.C:
				if maxTokens > 0 && loop.SessionOutputTokens() > maxTokens {
					fmt.Fprintln(os.Stderr, "drover-code: watchdog: token budget exceeded")
					writeHeadlessResultFile(workDir, loop, false, 0, "token budget exceeded (watchdog)")
					exitHeadless(4, "failed")
				}
			}
		}
	}()

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
			} else if cmdName == "score" {
				if len(parts) < 2 {
					fmt.Fprintln(os.Stderr, "Usage: /score <value> [comment...]")
					return false
				}
				val, err := strconv.ParseFloat(parts[1], 64)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Invalid score: %v\n", err)
					return false
				}
				comment := strings.Join(parts[2:], " ")
				traceID := loop.LastTraceID()
				if traceID == "" {
					fmt.Fprintln(os.Stderr, "No previous trace to score.")
					return false
				}
				telemetry.TracerFrom(ctx).Score(traceID, "user_feedback", val, telemetry.ScoreSourceHuman, comment)
				fmt.Fprintf(os.Stderr, "Score %v recorded for trace %s\n", val, traceID)
				return false
			}

			if expanded, cmdDef, err := cmdExec.EvaluateAndExpand(ctx, cmdName, parts[1:]); err == nil {
				input = expanded
				// Handle model/agent overrides if set
				if cmdDef.Model != "" {
					loop.SetDriver(agent.NewAnthropicInferenceDriver(client.WithModel(cmdDef.Model)))
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

		headlessSession.userMessage(input)
		err := loop.Run(ctx, input)
		sumTurns += loop.LastRunTurns()
		if err != nil {
			hadErr = true
			lastAgentErr = err
			exitCode = headlessExitCode(err)
			fmt.Fprintf(os.Stderr, "drover-code: agent error: %v\n", err)
			return true
		}
		for {
			n, err := feedBackgroundResults(ctx, loop, bashTool, backgroundDone)
			sumTurns += n
			if err == nil {
				break
			}
			hadErr = true
			lastAgentErr = err
			exitCode = headlessExitCode(err)
			fmt.Fprintf(os.Stderr, "drover-code: agent error: %v\n", err)
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
			exitHeadless(2, "failed")
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
			exitHeadless(2, "failed")
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
		exitHeadless(exitCode, "failed")
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			exitHeadless(4, "failed")
		}
		exitHeadless(1, "failed")
	}
	headlessSession.finish("completed")
}

func feedBackgroundResults(ctx context.Context, loop *agent.Loop, bashTool *shellpkg.Bash, done <-chan shellpkg.Notice) (int, error) {
	if bashTool == nil {
		return 0, nil
	}
	var turns int
	for {
		if bashTool.Running() == 0 {
			select {
			case res := <-done:
				n, err := runBackgroundResult(ctx, loop, res)
				turns += n
				if err != nil {
					return turns, err
				}
				continue
			default:
				return turns, nil
			}
		}
		select {
		case <-ctx.Done():
			bashTool.StopAll()
			return turns, ctx.Err()
		case res := <-done:
			n, err := runBackgroundResult(ctx, loop, res)
			turns += n
			if err != nil {
				return turns, err
			}
		}
	}
}

func runBackgroundResult(ctx context.Context, loop *agent.Loop, res shellpkg.Notice) (int, error) {
	fmt.Fprintf(os.Stderr, "\n%s\n", res.Output)
	headlessSession.userMessage(res.Output)
	err := loop.Run(ctx, res.Output)
	return loop.LastRunTurns(), err
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
