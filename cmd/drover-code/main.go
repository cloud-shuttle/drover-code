package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/cloud-shuttle/drover-code/internal/agent"
	"github.com/cloud-shuttle/drover-code/internal/api"
	"github.com/cloud-shuttle/drover-code/internal/commands"
	"github.com/cloud-shuttle/drover-code/internal/config"
	"github.com/cloud-shuttle/drover-code/internal/convo"
	"github.com/cloud-shuttle/drover-code/internal/mcp"
	"github.com/cloud-shuttle/drover-code/internal/permissions"
	"github.com/cloud-shuttle/drover-code/internal/session"
	"github.com/cloud-shuttle/drover-code/internal/telemetry"
	"github.com/cloud-shuttle/drover-code/internal/tools"
	browserpkg "github.com/cloud-shuttle/drover-code/internal/tools/browser"
	delegatepkg "github.com/cloud-shuttle/drover-code/internal/tools/delegate"
	explorepkg "github.com/cloud-shuttle/drover-code/internal/tools/explore"
	fspkg "github.com/cloud-shuttle/drover-code/internal/tools/fs"
	shellpkg "github.com/cloud-shuttle/drover-code/internal/tools/shell"
	"github.com/cloud-shuttle/drover-code/internal/tui"
	"github.com/cloud-shuttle/drover-code/internal/warden"
	"github.com/cloud-shuttle/drover-code/pkg/guardclient"
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

	workDir := mustGetwd()

	cfg := loadConfig(workDir)
	settings := cfg.Get()

	// Provide a persistence function for theme changes (writes back to project settings).
	// This enables "last-used theme per project" (saved into .claude/settings.json).
	themeSaveFn := func(mode, palette string) error {
		delta := config.Settings{}
		delta.Theme.Mode = mode
		if palette != "" {
			delta.Theme.Palette = palette
		}
		return cfg.Save(delta)
	}

	if startupFlags.CoordinatorRemote {
		settings.CoordinatorRemote = true
	}
	if startupFlags.AcceptCmd != "" {
		settings.AcceptCmd = startupFlags.AcceptCmd
	}
	if startupFlags.Verbose {
		settings.Verbose = true
	}
	if sessionResumeRequested() && !localSessionEnabled(settings) {
		fmt.Fprintln(os.Stderr, "drover-code: -c and --resume are for the local TUI and headless run")
		os.Exit(2)
	}
	var sessionPicker []tui.SessionRow
	if startupFlags.ResumeList && startupFlags.ResumeID == "" {
		if wantsHeadlessMode() {
			printSessionList(workDir)
			return
		}
		rows, err := listSavedSessions(workDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "drover-code: %v\n", err)
			os.Exit(2)
		}
		if len(rows) == 0 {
			fmt.Println("No saved sessions in this directory yet. A session is stored after the first completed reply. Quit, then run drover-code -c or drover-code --resume.")
			return
		}
		sessionPicker = rows
	}

	modelStr := coalesce(os.Getenv("ANTHROPIC_MODEL"), settings.Model, defaultModel)
	if api.ProviderFromEnv() == api.ProtocolOpenAI {
		if ov := api.ModelFromEnv(api.ProtocolOpenAI); ov != "" {
			modelStr = ov
		}
	}

	undercoverActive := resolveUndercoverMode(settings, workDir)
	sysPrompt := buildSystemPrompt(workDir, cfg.SystemInjection(), undercoverActive)

	client := requireLLMClient(modelStr)
	mgr := convo.NewManagerWithSystem(sysPrompt)
	config.ApplyConvoHeuristics(mgr, settings)
	config.ApplyTheme(settings) // Theme System foundation
	registry := tools.NewRegistry()
	tools.RegisterAll(registry, workDir)
	registerLocalHarness(registry, workDir)
	if localSessionEnabled(settings) {
		if b, ok := registry.Get("bash").(*shellpkg.Bash); ok {
			b.AllowBackground = true
			if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
				b.Sandbox = true
			}
		}
		// ponytail: explore and delegate use the process client, not a later Loop.SetDriver override; follow that override when a command must run on its own model.
		registry.Register(&explorepkg.Agent{
			Ask: func(ctx context.Context, task string) (string, error) {
				return agent.Explore(ctx, agent.NewAnthropicInferenceDriver(client), workDir, task)
			},
		})
		registry.Register(&delegatepkg.Agent{
			Ask: func(ctx context.Context, task string) (string, error) {
				return agent.Delegate(ctx, agent.NewAnthropicInferenceDriver(client), workDir, task, agent.DelegateOpts{
					Sandbox: runtime.GOOS == "darwin" || runtime.GOOS == "linux",
					Gopls:   true,
				})
			},
		})
		br := &browserpkg.Browser{}
		registry.Register(br)
		defer br.Close()
		if w, ok := registry.Get("write_file").(*fspkg.WriteFile); ok {
			w.Gopls = true
		}
		if e, ok := registry.Get("edit_file").(*fspkg.EditFile); ok {
			e.Gopls = true
		}
		prompt := mgr.SystemPrompt() + "\n\nexplore searches this tree in a separate read-only pass and returns only a short file map. Use it for a broad question about where code lives. Do not use it to edit files or run commands.\n\ndelegate runs one coding task in a separate loop and returns a short result. Use it for a chunk of work that would flood this conversation. The child can read, edit, and run bash. It cannot ask the user or start another delegate.\n\nbrowser opens a local headless Chrome. Use open, then snapshot, click, type, or screenshot. It can reach the network. bash cannot. Pages are http and https.\n\nAfter write_file or edit_file changes a .go file, the tool result includes gopls errors and warnings for that file.\n"
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			prompt += "\nbash is sandboxed in this session. It can read files and write only inside the working tree and a private temp directory. Network is blocked, so downloads and package fetches fail.\n"
		}
		mgr.SetSystemPrompt(prompt)
	}

	dreamStore, dreamWorker := setupDream(settings, workDir, client)

	lf := telemetry.New(telemetry.ConfigFromEnv())
	defer lf.Flush()

	sessionID := session.NewID()
	var rec *session.Recorder
	if localSessionEnabled(settings) {
		store := session.Open(session.DefaultRoot())
		if len(sessionPicker) == 0 && (startupFlags.ResumeID != "" || startupFlags.Continue) {
			tr, err := loadSavedSession(store, workDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "drover-code: %v\n", err)
				os.Exit(2)
			}
			for _, msg := range tr.Messages {
				mgr.Append(msg)
			}
			sessionID = tr.ID
		}
		rec = session.NewRecorder(store, sessionID, workDir, mgr.Messages)
	}
	baseCtx := telemetry.WithSessionID(context.Background(), sessionID)

	// Phase 4: Linear Telemetry Context
	if issueID := telemetry.ExtractIssueID(workDir, startupFlags.LinearIssue); issueID != "" {
		baseCtx = telemetry.WithLinearIssue(baseCtx, issueID)
	}
	if linClient := telemetry.NewLinearClient(); linClient != nil {
		baseCtx = telemetry.WithLinearClient(baseCtx, linClient)
	}

	ctx, cancel := signal.NotifyContext(telemetry.WithTracer(baseCtx, lf), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var mcpNotes []string
	for _, w := range mcp.RegisterAll(ctx, registry, settings) {
		fmt.Fprintf(os.Stderr, "mcp: %s\n", w)
		mcpNotes = append(mcpNotes, w)
	}

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
		runHeadless(ctx, client, mgr, registry, settings, workDir, dreamWorker, cmdExecutor, rec)

	case envTruthy("DROVER_CODE_IDE_BRIDGE"):
		runBridgeMode(ctx, client, mgr, registry, settings, workDir, dreamWorker)

	case startupFlags.CloudMode:
		runCloudMode(ctx, workDir, settings)

	case envTruthy("DROVER_CODE_COORDINATOR_MODE") || settings.CoordinatorMode || settings.CoordinatorRemote:
		runCoordinatorMode(ctx, client, registry, modelStr, workDir, settings, dreamWorker)

	default:
		runTUI(ctx, client, mgr, registry, modelStr, settings, workDir, dreamWorker, dreamStore, cmdExecutor, themeSaveFn, mcpNotes, rec, sessionPicker)
	}
}

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

// ── Helpers ───────────────────────────────────────────────────────────────────

func loadConfig(workDir string) *config.Loader {
	cfg := config.NewLoader(workDir)
	if err := cfg.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config: %v\n", err)
	}
	return cfg
}

func printEvents(ch <-chan agent.Event) {
	for event := range ch {
		recordSessionObserve(event)
		switch e := event.(type) {
		case agent.TextDeltaEvent:
			fmt.Print(e.Text)
		case agent.ToolStartEvent:
			fmt.Fprintf(os.Stderr, "\n⚙  %s\n", e.InputSummary)
		case agent.TodoEvent:
			fmt.Fprintf(os.Stderr, "\n%s\n", e.Text)
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

func sessionResumeRequested() bool {
	return startupFlags.Continue || startupFlags.ResumeID != "" || startupFlags.ResumeList
}

func localSessionEnabled(settings config.Settings) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DROVER_CODE_PERMISSION_PRESET")), permissions.PresetUnikernel) {
		return false
	}
	if startupFlags.CloudMode || startupFlags.CoordinatorRemote {
		return false
	}
	if envTruthy("DROVER_CODE_IDE_BRIDGE") || envTruthy("DROVER_CODE_COORDINATOR_MODE") {
		return false
	}
	if settings.CoordinatorMode || settings.CoordinatorRemote {
		return false
	}
	return true
}

func loadSavedSession(store *session.Store, workDir string) (session.Transcript, error) {
	var (
		tr  session.Transcript
		err error
	)
	if startupFlags.ResumeID != "" {
		tr, err = store.Load(workDir, startupFlags.ResumeID)
	} else {
		tr, err = store.LoadLatest(workDir)
	}
	if err != nil {
		return session.Transcript{}, err
	}
	tr.Messages = session.CloseDangling(tr.Messages)
	return tr, nil
}

func listSavedSessions(workDir string) ([]tui.SessionRow, error) {
	root := session.DefaultRoot()
	if root == "" {
		return nil, fmt.Errorf("cannot find the home directory for saved sessions")
	}
	rows, err := session.Open(root).List(workDir, 20)
	if err != nil {
		return nil, err
	}
	out := make([]tui.SessionRow, len(rows))
	for i, row := range rows {
		out[i] = tui.SessionRow{
			ID:      row.ID,
			When:    row.Updated.Local().Format("2006-01-02 15:04"),
			Preview: row.Preview,
		}
	}
	return out, nil
}

func printSessionList(workDir string) {
	root := session.DefaultRoot()
	if root == "" {
		fmt.Fprintln(os.Stderr, "drover-code: cannot find the home directory for saved sessions")
		os.Exit(2)
	}
	rows, err := session.Open(root).List(workDir, 20)
	if err != nil {
		fmt.Fprintf(os.Stderr, "drover-code: %v\n", err)
		os.Exit(2)
	}
	if len(rows) == 0 {
		fmt.Println("No saved sessions in this directory yet. A session is stored after the first completed reply. Quit, then run drover-code -c or drover-code --resume.")
		return
	}
	for _, row := range rows {
		fmt.Printf("%s  %s  %s\n", row.ID, row.Updated.Local().Format("2006-01-02 15:04"), row.Preview)
	}
}

// wantsHeadlessMode returns true when headless (non-TUI) execution is requested.

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
