package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
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
	"github.com/cloudshuttle/drover-code/internal/commands"
	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/coordinator"
	"github.com/cloudshuttle/drover-code/internal/dream"
	github "github.com/cloudshuttle/drover-code/internal/github"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/session"
	"github.com/cloudshuttle/drover-code/internal/skills"
	"github.com/cloudshuttle/drover-code/internal/tools"
	askpkg "github.com/cloudshuttle/drover-code/internal/tools/ask"
	shellpkg "github.com/cloudshuttle/drover-code/internal/tools/shell"
	todopkg "github.com/cloudshuttle/drover-code/internal/tools/todo"
	webpkg "github.com/cloudshuttle/drover-code/internal/tools/web"
	"github.com/cloudshuttle/drover-code/internal/tui"
	"github.com/cloudshuttle/drover-code/internal/undercover"
)

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
	themeSaveFn func(mode, palette string) error,
	notes []string,
	rec *session.Recorder,
	picker []tui.SessionRow,
) {
	if ds != nil {
		if inj := dream.BuildInjection(ds, 5); inj != "" {
			mgr.SetSystemPrompt(mgr.SystemPrompt() + "\n\n" + inj)
		}
	}
	if dw != nil {
		dw.Start(ctx)
	}

	mgr.SetSystemPrompt(mgr.SystemPrompt() + "\n\nask_user asks the person one question and waits for their typed answer. Use it when you need a decision you cannot infer. One question at a time.\n")
	prog := tui.NewProgram(ctx, client, mgr, registry, modelStr, settings, workDir, cmdExec, themeSaveFn)
	registry.Register(&askpkg.User{Ask: prog.AskUser})
	if b, ok := registry.Get("bash").(*shellpkg.Bash); ok && b.AllowBackground {
		b.SetOnDone(func(n shellpkg.Notice) {
			prog.Send(tui.BackgroundBashMsg{Running: n.Running, Output: n.Output, Done: n.Done})
		})
		defer b.StopAll()
	}
	if rec != nil {
		prog.SetPersist(rec.Save)
		prog.SetOnClear(rec.Rotate)
		if len(mgr.Messages()) > 0 {
			prog.Restore(mgr.Messages(), "Continued session "+rec.ID())
		}
	}
	if len(picker) > 0 && rec != nil {
		store := session.Open(session.DefaultRoot())
		prog.OfferSessions(picker, notes, func(id string) (string, []api.Message, error) {
			tr, err := store.Load(workDir, id)
			if err != nil {
				return "", nil, err
			}
			tr.Messages = session.CloseDangling(tr.Messages)
			mgr.Reset()
			for _, msg := range tr.Messages {
				mgr.Append(msg)
			}
			rec.Use(tr.ID)
			return tr.ID, mgr.Messages(), nil
		})
	}
	if len(picker) == 0 {
		for _, note := range notes {
			prog.Note(note)
		}
	}
	if err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	flushDreamWorker(dw, "tui", mgr.Messages())
}

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
	driver := agent.NewAnthropicInferenceDriver(client)
	executor := agent.NewDefaultToolExecutor(registry, eng, eventCh)
	loop := agent.NewLoop(driver, mgr, executor, registry, eventCh)
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
		innerDriver := agent.NewAnthropicInferenceDriver(client)
		innerExecutor := agent.NewDefaultToolExecutor(registry, eng, innerCh)
		innerLoop := agent.NewLoop(innerDriver, mgr, innerExecutor, registry, innerCh)
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

// ── Webhook server ─────────────────────────────────────────────────────────

func runWebhookServer() {
	ghToken := requireEnv("GITHUB_TOKEN")
	webhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	addr := coalesce(os.Getenv("WEBHOOK_ADDR"), ":8080")
	workBase := coalesce(os.Getenv("WEBHOOK_WORK_DIR"), "/tmp/drover-code-work")

	if err := os.MkdirAll(workBase, 0o755); err != nil {
		log.Fatalf("create work dir: %v", err)
	}

	modelStr := coalesce(os.Getenv("ANTHROPIC_MODEL"), defaultModel)
	if api.ProviderFromEnv() == api.ProtocolOpenAI {
		if ov := api.ModelFromEnv(api.ProtocolOpenAI); ov != "" {
			modelStr = ov
		}
	}
	client := requireLLMClient(modelStr)
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
	if os.Getenv("DROVER_CODE_PERMISSION_PRESET") != permissions.PresetUnikernel {
		b.WriteString("\n\nLocal tools: web_search finds pages, web_fetch reads one URL, read_skill loads one skill body, and todo replaces the session checklist. MCP tools are named mcp_{server}_{tool}.\n")
		if p := skills.Prompt(workDir); p != "" {
			b.WriteString("\n")
			b.WriteString(p)
		}
	}
	return b.String()
}

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

func registerLocalHarness(reg *tools.Registry, workDir string) {
	if os.Getenv("DROVER_CODE_PERMISSION_PRESET") == permissions.PresetUnikernel {
		return
	}
	reg.Register(webpkg.NewSearch())
	reg.Register(&skills.ReadTool{WorkDir: workDir})
	reg.Register(&todopkg.List{})
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
