package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/tools/fs"
	"github.com/cloudshuttle/drover-code/internal/tui/commandpalette"
	"github.com/cloudshuttle/drover-code/internal/tui/components/historyview"
	"github.com/cloudshuttle/drover-code/internal/tui/components/inputarea"
	"github.com/cloudshuttle/drover-code/internal/tui/components/liveregion"
	"github.com/cloudshuttle/drover-code/internal/tui/components/permissionprompt"
	"github.com/cloudshuttle/drover-code/internal/tui/components/statusbar"
	"github.com/cloudshuttle/drover-code/internal/tui/components/toolspinner"
	"github.com/cloudshuttle/drover-code/internal/tui/core"
	"github.com/cloudshuttle/drover-code/internal/tui/diff"
	"github.com/cloudshuttle/drover-code/internal/tui/history"
	"github.com/cloudshuttle/drover-code/internal/tui/historysearch"
	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

type slashItem struct {
	name string
	desc string
}

// RunFunc submits user text to the agent loop.
type RunFunc func(input string) tea.Cmd

type Model struct {
	width, height int

	// streamBuf is retained **only** for the final Glamour render of assistant turns (used in DoneEvent).
	// It must never be used for the live preview.
	//
	// The live streaming preview lives in a completely separate buffer inside LiveRegion
	// (see liveregion.livePreview) so it can be aggressively post-processed, truncated,
	// and buffered without affecting the committed history content.
	// LiveRegion owns the live preview + tool activity.
	// HistoryView owns conversation history rendering.
	streamBuf strings.Builder

	inputHistory *history.PersistentHistory
	historyIndex int
	savedInput   string

	inputFocused bool

	// dcode-007: Permission prompt components (source of truth)
	PermPrompt *permissionprompt.PermissionPrompt
	PermBatch  *permissionprompt.PermissionBatchPrompt
	askUser    *askUserPrompt

	diffModel   *diff.Model
	showingDiff bool

	searchModel   *historysearch.Model
	showingSearch bool

	commandPaletteModel   *commandpalette.Model
	showingCommandPalette bool

	// Components (dcode-003 and beyond)
	StatusBar   *statusbar.StatusBar
	Live        *liveregion.LiveRegion
	HistoryView *historyview.HistoryView
	InputArea   *inputarea.InputArea

	// Guard / risk state for StatusBar (real hooks + richer behavior)
	GuardRiskLevel  string // "normal", "caution", "high"
	GuardRiskReason string // short explanation when risk is elevated

	// extraPaletteCommands are registered by external layers (drover, etc.)
	// via RegisterPaletteCommands.
	extraPaletteCommands []commandpalette.Command

	// paletteProviders are functions that can dynamically contribute commands.
	paletteProviders []commandpalette.CommandProvider

	// paletteActionHandlers map ActionKey -> handler for custom semantic actions.
	paletteActionHandlers map[string]commandpalette.ActionHandler

	glamourRenderer *glamour.TermRenderer

	eventCh <-chan agent.Event
	// eventWaitArmed is true while one waitForEvent command is blocked on
	// eventCh. Bubble Tea runs commands concurrently, so a second reader
	// can commit a later token first ("P"+"ong" becomes "ongP").
	eventWaitArmed bool

	modelName         string
	workDir           string
	userName          string
	hostName          string
	totalInputTokens  int
	totalOutputTokens int
	agentBusy         bool
	runGen            int
	turnOpen          bool
	pendingBash       []string

	lastAPICallInput  int
	lastAPICallOutput int

	lastError string
	todoList  string

	compactionBanner string

	maxGlamourRunes   int
	maxHistoryDisplay int

	runFunc         RunFunc
	runCancel       context.CancelFunc
	lastPauseReason string // optional reason from /pause "reason"
	compactFn       func() error
	convoMgr        *convo.Manager
	themeSaveFn     func(mode, palette string) error // for persisting theme changes
	onClear         func()
	planOn          bool
	planToggle      func(on bool)

	showingSessions bool
	sessionRows     []SessionRow
	sessionQuery    string
	sessionCursor   int
	loadSession     func(id string) error
}

// SetRunCancel allows injecting the context cancellation function for the current run
func (m *Model) SetRunCancel(cancel context.CancelFunc) {
	m.runCancel = cancel
}

func New(eventCh <-chan agent.Event, modelName, workDir, userName, hostName string) *Model {
	hist, err := history.NewPersistentHistory(workDir)
	if err != nil {
		hist = &history.PersistentHistory{}
	}

	m := &Model{
		eventCh:           eventCh,
		modelName:         modelName,
		workDir:           workDir,
		userName:          userName,
		hostName:          hostName,
		inputFocused:      true,
		maxGlamourRunes:   readMaxGlamourRunesFromEnv(),
		maxHistoryDisplay: readMaxHistoryDisplayFromEnv(),
		inputHistory:      hist,
		historyIndex:      len(hist.Get()),
		GuardRiskLevel:    "normal", // default for StatusBar risk/guard indicator
	}

	// Components own their state after the InputArea consolidation
	m.StatusBar = statusbar.New(modelName)
	m.StatusBar.RiskLevel = m.GuardRiskLevel
	m.Live = liveregion.New()
	m.HistoryView = historyview.New()
	m.InputArea = inputarea.New()

	// Give InputArea the default slash commands (it is now the owner)
	defaults := defaultSlashCommands()
	names := make([]string, len(defaults))
	descs := make([]string, len(defaults))
	for i, c := range defaults {
		names[i] = c.name
		descs[i] = c.desc
	}
	m.InputArea.RegisterSlashCommands(names, descs)

	return m
}

func readMaxGlamourRunesFromEnv() int {
	s, ok := os.LookupEnv("DROVER_CODE_TUI_MAX_GLAMOUR_RUNES")
	if !ok {
		return 0
	}
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func readMaxHistoryDisplayFromEnv() int {
	s := strings.TrimSpace(os.Getenv("DROVER_CODE_TUI_MAX_HISTORY_DISPLAY"))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.armEventWait(),
	)
}

// armEventWait starts the single event reader. A nil return means one is
// already blocked on eventCh.
func (m *Model) armEventWait() tea.Cmd {
	if m.eventWaitArmed || m.eventCh == nil {
		return nil
	}
	m.eventWaitArmed = true
	return waitForEvent(m.eventCh)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		cmds = append(cmds, m.armEventWait())
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		if m.showingSessions {
			return m.updateSessionPick(msg)
		}
		if m.showingSearch && m.searchModel != nil {
			var newModel tea.Model
			var cmd tea.Cmd
			newModel, cmd = m.searchModel.Update(msg)
			m.searchModel = newModel.(*historysearch.Model)
			return m, cmd
		}

		if m.showingDiff && m.diffModel != nil {
			var newDiff tea.Model
			var cmd tea.Cmd
			newDiff, cmd = m.diffModel.Update(msg)
			*m.diffModel = newDiff.(diff.Model)

			switch msg.String() {
			case "q", "esc":
				m.showingDiff = false
				m.diffModel = nil
				m.lastError = "User cancelled interactive diff review."
				return m, nil
			case "enter", "ctrl+s":
				applier := diff.NewPatchApplier(m.workDir)
				_, err := applier.ApplyAcceptedHunks(
					m.diffModel.GetFilePath(),
					m.diffModel.GetHunks(),
				)

				if err != nil {
					m.lastError = fmt.Sprintf("Failed to apply diff: %v", err)
				} else {
					if m.PermPrompt != nil {
						select {
						case m.PermPrompt.DecisionCh <- agent.PermAppliedManually:
						default:
						}
						m.PermPrompt = nil
					}
				}

				m.showingDiff = false
				m.diffModel = nil
				return m, nil
			}
			return m, cmd
		}

		if m.showingCommandPalette && m.commandPaletteModel != nil {
			var newModel tea.Model
			var cmd tea.Cmd
			newModel, cmd = m.commandPaletteModel.Update(msg)
			m.commandPaletteModel = newModel.(*commandpalette.Model)
			return m, cmd
		}

		if m.PermPrompt != nil {
			cmd := m.handlePermissionKey(msg)
			return m, cmd
		}
		if m.PermBatch != nil {
			cmd := m.handlePermissionBatchKey(msg)
			return m, cmd
		}
		if m.askUser != nil && (msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlJ) {
			return m, m.answerAsk()
		}
		if m.askUser != nil && msg.Type == tea.KeyEsc {
			m.dismissAsk()
			return m, nil
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			m.clearAsk()
			if m.agentBusy {
				if m.runCancel != nil {
					m.runCancel()
					m.runCancel = nil
				}
				m.lastError = "Interrupting agent... waiting for graceful halt."
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyCtrlR:
			if !m.agentBusy {
				m.searchModel = historysearch.New(m.inputHistory.Get(), m.width, m.height)
				m.showingSearch = true
			}
			return m, nil

		case tea.KeyCtrlK:
			// Allow palette even while busy so "pause" (and other safe actions) are discoverable
			// via Ctrl+K during a long-running agent turn.
			cmds := m.buildCommandPaletteCommands()
			m.commandPaletteModel = commandpalette.NewWithCommands(cmds, m.width, m.height)
			m.showingCommandPalette = true
			return m, nil
		case tea.KeyEnter, tea.KeyCtrlJ:
			return m, m.takeInput(false)
		case tea.KeyCtrlG:
			return m, m.takeInput(true)
		case tea.KeyEsc:
			m.InputArea.ClearAutocomplete()
			if m.showingCommandPalette {
				m.showingCommandPalette = false
				m.commandPaletteModel = nil
				return m, nil
			}
		case tea.KeyUp:
			if m.InputArea.AutoActive() {
				m.InputArea.SetAutoIndex(m.InputArea.AutoIndex() - 1)
				return m, nil
			} else if !m.InputArea.AutoActive() && m.InputArea.Textarea.Line() == 0 {
				histEntries := m.inputHistory.Get()
				if len(histEntries) > 0 && m.historyIndex > 0 {
					if m.historyIndex == len(histEntries) {
						m.savedInput = m.InputArea.Value()
					}
					m.historyIndex--
					m.InputArea.SetValue(histEntries[m.historyIndex])
					m.InputArea.CursorEnd()
					return m, nil
				}
			}
		case tea.KeyDown:
			if m.InputArea.AutoActive() {
				m.InputArea.SetAutoIndex(m.InputArea.AutoIndex() + 1)
				return m, nil
			} else if !m.InputArea.AutoActive() && m.InputArea.Textarea.Line() == m.InputArea.Textarea.LineCount()-1 {
				histEntries := m.inputHistory.Get()
				if m.historyIndex < len(histEntries) {
					m.historyIndex++
					if m.historyIndex == len(histEntries) {
						m.InputArea.SetValue(m.savedInput)
					} else {
						m.InputArea.SetValue(histEntries[m.historyIndex])
					}
					m.InputArea.CursorEnd()
					return m, nil
				}
			}
		case tea.KeyTab:
			if m.InputArea.AcceptAutocomplete() {
				return m, nil
			}
		case tea.KeyShiftTab:
			m.togglePlanMode()
			return m, nil
		case tea.KeyPgUp, tea.KeyPgDown:
			var vpCmd tea.Cmd
			if m.HistoryView != nil {
				m.HistoryView, vpCmd = m.HistoryView.Update(msg)
				cmds = append(cmds, vpCmd)
				return m, tea.Batch(cmds...)
			}
			// Legacy viewport path removed (HistoryView owns scrolling)
			return m, tea.Batch(cmds...)
		}

		// Forward to InputArea (now owns the textarea and autocomplete state).
		// InputArea performs its own sanitization on every Update to strip terminal
		// OSC responses, CSI reports, and other garbage that can leak from the pty
		// (especially "]11;rgb:..." color query replies on startup).
		_, taCmd := m.InputArea.Update(msg)
		cmds = append(cmds, taCmd)

		m.InputArea.UpdateAutocomplete()
		return m, tea.Batch(cmds...)

	case historysearch.SelectedMsg:
		m.InputArea.SetValue(msg.Entry)
		m.InputArea.CursorEnd()
		m.showingSearch = false
		m.searchModel = nil
		return m, nil

	case historysearch.CancelMsg:
		m.showingSearch = false
		m.searchModel = nil
		return m, nil

	case commandpalette.SelectedMsg:
		m.showingCommandPalette = false
		m.commandPaletteModel = nil

		if msg.ActionKey != "" {
			// First check for externally registered handlers (exact match)
			if handler, ok := m.paletteActionHandlers[msg.ActionKey]; ok && handler != nil {
				if cmd := handler(msg.ActionKey); cmd != nil {
					return m, cmd
				}
			}
			// Support prefix-registered handlers (e.g. Register "theme:" to catch "theme:default:dark")
			for regKey, h := range m.paletteActionHandlers {
				if regKey != "" && strings.HasSuffix(regKey, ":") && strings.HasPrefix(msg.ActionKey, regKey) {
					if cmd := h(msg.ActionKey); cmd != nil {
						return m, cmd
					}
					break
				}
			}
			return m, m.executePaletteAction(msg.ActionKey)
		}

		// Default: text injection for slash commands
		m.InputArea.SetValue("/" + msg.Name + " ")
		m.InputArea.CursorEnd()
		return m, nil

	case commandpalette.CancelMsg:
		m.showingCommandPalette = false
		m.commandPaletteModel = nil
		return m, nil

	case BackgroundBashMsg:
		if m.StatusBar != nil {
			m.StatusBar.BackgroundJobs = msg.Running
		}
		if !msg.Done || msg.Output == "" {
			return m, nil
		}
		if m.agentPaused() {
			m.noteBackground(msg.Output)
			return m, nil
		}
		if m.agentBusy || m.turnOpen {
			m.pendingBash = append(m.pendingBash, msg.Output)
			return m, nil
		}
		return m, m.submitInput(msg.Output)

	case agentMsg:
		m.eventWaitArmed = false
		cmd := m.handleAgentEvent(msg.event)
		cmds = append(cmds, m.armEventWait())
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case agentRunCompleteMsg:
		if msg.gen != 0 && msg.gen != m.runGen {
			return m, nil
		}
		m.turnOpen = false
		m.agentBusy = false
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.lastError = "Agent paused by user."
				reasonNote := ""
				pausedReasonForBar := m.lastPauseReason
				if m.lastPauseReason != "" {
					reasonNote = ": " + m.lastPauseReason
					m.lastPauseReason = "" // consume for note
				}
				m.HistoryView.AppendTurn(core.RenderedTurn{
					Role:    "system",
					Content: fmt.Sprintf("(/pause%s) Agent interrupted. Waiting for new instructions...", reasonNote),
				})
				if m.StatusBar != nil {
					m.StatusBar.Paused = true
					m.StatusBar.PausedReason = pausedReasonForBar
				}
				for _, text := range m.pendingBash {
					m.noteBackground(text)
				}
				m.pendingBash = nil
				return m, tea.Batch(cmds...)
			} else {
				m.lastError = msg.err.Error()

				// Real Guard hook: surface blocks from the outer drover-guard in the StatusBar
				if strings.Contains(msg.err.Error(), "Governance Policy") || strings.Contains(msg.err.Error(), "Drover Guard") {
					m.SetGuardRisk("high", "command blocked by guard")
				}
			}
		}

		for {
			next, ok := m.popFollowUp(msg.err == nil)
			if !ok || m.agentBusy {
				break
			}
			if cmd := m.submitInput(next); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		return m, tea.Batch(cmds...)

	case compactCompleteMsg:
		m.agentBusy = false
		m.compactionBanner = ""
		if msg.err != nil {
			m.lastError = msg.err.Error()
		} else {
			m.lastError = ""
			m.HistoryView.AppendTurn(core.RenderedTurn{
				Role:    "user",
				Content: "(/compact) Older turns were summarised into one context message; recent messages kept.",
			})
		}
		m.scrollToBottom()

		cmds = append(cmds, m.armEventWait())
		for {
			next, ok := m.InputArea.Dequeue()
			if !ok || m.agentBusy {
				break
			}
			if cmd := m.submitInput(next); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		// dcode-005 consolidation: LiveRegion owns its spinners
		if m.Live != nil {
			for idx, ts := range m.Live.ActiveTools {
				var cmd tea.Cmd
				m.Live.ActiveTools[idx].Spinner, cmd = ts.Spinner.Update(msg)
				cmds = append(cmds, cmd)
			}
		}

		// Legacy tool spinner loop removed (LiveRegion is now the sole owner)
		return m, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleAgentEvent(ev agent.Event) tea.Cmd {
	switch e := ev.(type) {
	case agent.TextDeltaEvent:
		m.streamBuf.WriteString(e.Text)

		// dcode / remediation #7: LiveRegion (via AppendDelta) now fully owns
		// incremental sanitization + tail context + preview buffering + throttling.
		// Model only writes to the final streamBuf (for DoneEvent Glamour render)
		// and tells the live component about the raw delta.
		if m.Live != nil {
			m.Live.Streaming = true
			m.Live.AppendDelta(e.Text)
		}

		m.scrollToBottom()

	case agent.ToolStartEvent:
		// Flush any pending assistant text before showing tool activity
		if m.Live != nil {
			m.Live.ForceFlushPending()
		}

		// LiveRegion is the sole owner of active tool spinners
		ts := toolspinner.New(e.Name, e.InputSummary)
		m.Live.ActiveTools[e.CallIndex] = ts
		m.Live.ToolOrder = append(m.Live.ToolOrder, e.CallIndex)
		m.scrollToBottom()
		return ts.Spinner.Tick

	case agent.TodoEvent:
		m.todoList = e.Text

	case agent.ToolDoneEvent:
		// LiveRegion is the sole owner of active tools + completed tools
		if _, ok := m.Live.ActiveTools[e.CallIndex]; ok {
			done := core.CompletedTool{
				Name:    e.Name,
				Summary: e.OutputSummary,
				IsError: e.IsError,
			}
			m.Live.CompletedTools = append(m.Live.CompletedTools, done)

			delete(m.Live.ActiveTools, e.CallIndex)
			for i, idx := range m.Live.ToolOrder {
				if idx == e.CallIndex {
					m.Live.ToolOrder = append(m.Live.ToolOrder[:i], m.Live.ToolOrder[i+1:]...)
					break
				}
			}
		}

	case agent.AskUserEvent:
		if m.askUser != nil {
			m.dismissAsk()
		}
		m.askUser = &askUserPrompt{Question: e.Question, ReplyCh: e.ReplyCh}
		m.lastError = ""

	case agent.PermissionRequestEvent:
		// Deeper Guard heuristics (beyond simple tool names)
		level, reason := m.assessPermissionRisk(e.ToolName, e.Input, e.Summary)
		if level != "" && level != "normal" {
			m.SetGuardRisk(level, reason)
		}

		if e.ToolName == "edit_file" {
			filePath, diffStr, err := fs.PreviewEdit(m.workDir, e.Input)
			if err == nil && diffStr != "" && diffStr != "no changes" {
				dm := diff.NewDiffModel(filePath, diffStr)
				m.diffModel = &dm
				m.showingDiff = true

				// dcode-007: PermissionPrompt component (legacy dual-state fully removed)
				m.PermPrompt = &permissionprompt.PermissionPrompt{
					ToolName:   e.ToolName,
					Summary:    e.Summary,
					InputJSON:  e.Input,
					DecisionCh: e.DecisionCh,
				}
				m.PermBatch = nil
				m.scrollToBottom()
				return nil
			}
		}

		// dcode-007: PermissionPrompt component
		m.PermPrompt = &permissionprompt.PermissionPrompt{
			ToolName:   e.ToolName,
			Summary:    e.Summary,
			InputJSON:  e.Input,
			DecisionCh: e.DecisionCh,
		}
		m.PermBatch = nil

	case agent.PermissionBatchRequestEvent:
		// dcode-007: PermissionBatchPrompt component (legacy dual-state removed)
		m.PermBatch = &permissionprompt.PermissionBatchPrompt{
			Items:      e.Items,
			DecisionCh: e.DecisionCh,
		}
		m.PermPrompt = nil

	case agent.UsageEvent:
		m.totalInputTokens = e.TotalInputTokens
		m.totalOutputTokens = e.TotalOutputTokens
		m.lastAPICallInput = e.InputTokens
		m.lastAPICallOutput = e.OutputTokens

		// dcode-003: dual-state sync to StatusBar component
		if m.StatusBar != nil {
			m.StatusBar.InputTokens = e.TotalInputTokens
			m.StatusBar.OutputTokens = e.TotalOutputTokens
			m.StatusBar.RiskLevel = m.GuardRiskLevel
			m.StatusBar.RiskReason = m.GuardRiskReason
		}

	case agent.HeartbeatEvent:
		// Orchestrator telemetry only; no TUI update.

	case agent.CompactionStartEvent:
		m.compactionBanner = fmt.Sprintf("Summarizing context (%d/%d)… ~%dk est. tokens",
			e.Round, e.MaxRounds, (e.EstimatedTokensBefore+999)/1000)

	case agent.CompactionDoneEvent:
		m.compactionBanner = ""
		if e.Err != nil {
			m.lastError = fmt.Sprintf("compaction: %v", e.Err)
		}

	case agent.DoneEvent:
		raw := m.streamBuf.String()
		if raw != "" {
			rendered := m.renderMarkdown(raw)

			// LiveRegion is the sole source for completed tools for this turn
			var completed []core.CompletedTool
			if drained := m.Live.DrainCompletedTools(); len(drained) > 0 {
				completed = drained
			}

			// HistoryView is the sole owner of conversation history
			m.HistoryView.AppendTurn(core.RenderedTurn{
				Role:    "assistant",
				Content: rendered,
				Tools:   completed,
			})
			m.HistoryView.GotoBottom()
		}

		m.streamBuf.Reset()

		// Make sure any last pending streaming tokens are flushed before we clear the live area
		if m.Live != nil {
			m.Live.ForceFlushPending()
		}

		m.Live.Streaming = false
		m.agentBusy = false

		if m.StatusBar != nil {
			m.StatusBar.AgentBusy = false
		}

		// LiveRegion is the sole owner of live state
		m.Live.ResetLiveStreaming()
		m.Live.ActiveTools = make(map[int]*toolspinner.ToolSpinner)
		m.Live.ToolOrder = nil
		m.Live.CompletedTools = nil

		m.scrollToBottom()

	case agent.ErrorEvent:
		m.clearAsk()
		m.lastError = e.Err.Error()
		m.agentBusy = false
		_ = m.InputArea.DrainQueue() // Clear the queue on error
		m.Live.Streaming = false
		m.streamBuf.Reset()
		m.compactionBanner = ""

		if m.StatusBar != nil {
			m.StatusBar.AgentBusy = false
		}

		// LiveRegion is the sole owner of live state
		m.Live.Streaming = false
		m.Live.ResetLiveStreaming()
		m.Live.ActiveTools = make(map[int]*toolspinner.ToolSpinner)
		m.Live.ToolOrder = nil
		m.Live.CompletedTools = nil
	}

	return nil
}

func (m *Model) handlePermissionKey(msg tea.KeyMsg) tea.Cmd {
	if m.PermPrompt == nil {
		return nil
	}

	switch msg.String() {
	case "y", "Y":
		m.PermPrompt.Respond(agent.PermAllow)
		m.PermPrompt = nil
	case "a", "A":
		m.PermPrompt.Respond(agent.PermAlwaysAllow)
		m.PermPrompt = nil
	case "n", "N", "q", tea.KeyEsc.String():
		m.PermPrompt.Respond(agent.PermDeny)
		m.PermPrompt = nil
	}
	return nil
}

func (m *Model) handlePermissionBatchKey(msg tea.KeyMsg) tea.Cmd {
	if m.PermBatch == nil {
		return nil
	}

	switch msg.String() {
	case "y", "Y":
		m.PermBatch.Respond(agent.PermAllow)
		m.PermBatch = nil
	case "a", "A":
		m.PermBatch.Respond(agent.PermAlwaysAllow)
		m.PermBatch = nil
	case "n", "N", "q", tea.KeyEsc.String():
		m.PermBatch.Respond(agent.PermDeny)
		m.PermBatch = nil
	}
	return nil
}

// takeInput submits the draft. sendNow cancels a live turn and puts the draft
// at the front of the queue so it runs as soon as the turn stops.
func (m *Model) takeInput(sendNow bool) tea.Cmd {
	input := strings.TrimSpace(m.InputArea.Value())
	if input == "" {
		return nil
	}
	m.inputHistory.Add(input)
	m.historyIndex = len(m.inputHistory.Get())
	m.savedInput = ""
	m.InputArea.Reset()
	m.InputArea.ClearAutocomplete()
	m.lastError = ""

	in := strings.ToLower(input)
	if in == "/quit" || in == "/exit" {
		return tea.Quit
	}
	if in == "/history" {
		if !m.agentBusy {
			m.searchModel = historysearch.New(m.inputHistory.Get(), m.width, m.height)
			m.showingSearch = true
		}
		return nil
	}
	if m.agentBusy {
		if sendNow {
			m.InputArea.QueueFront(input)
			if m.runCancel != nil {
				m.runCancel()
				m.runCancel = nil
			}
			m.lastError = "Sending when the current turn stops."
		} else {
			m.InputArea.Queue(input)
		}
		m.scrollToBottom()
		return nil
	}
	return m.submitInput(input)
}

func (m *Model) submitInput(input string) tea.Cmd {
	if cmd, handled := m.handleBuiltinSlash(input); handled {
		return cmd
	}

	// HistoryView is the sole owner of conversation history
	m.HistoryView.AppendTurn(core.RenderedTurn{Role: "user", Content: input})
	m.HistoryView.GotoBottom()
	m.agentBusy = true
	m.Live.Streaming = true
	// New input clears any previous paused state
	m.lastPauseReason = ""
	if m.StatusBar != nil {
		m.StatusBar.Paused = false
		m.StatusBar.PausedReason = ""
	}

	// dcode-005 consolidation: sync to components, clear previous live state on new input
	if m.StatusBar != nil {
		m.StatusBar.AgentBusy = true
	}
	if m.Live != nil {
		m.Live.Streaming = false
		m.Live.ResetLiveStreaming()
		// Optionally clear any stale active tools on new user turn (usually DoneEvent should have done this)
		if len(m.Live.ActiveTools) > 0 {
			m.Live.ActiveTools = make(map[int]*toolspinner.ToolSpinner)
			m.Live.ToolOrder = nil
		}
	}

	m.scrollToBottom()

	if m.runFunc != nil {
		cmd := m.runFunc(input)
		if cmd == nil {
			return nil
		}
		m.runGen++
		gen := m.runGen
		m.turnOpen = true
		return func() tea.Msg {
			msg := cmd()
			if done, ok := msg.(agentRunCompleteMsg); ok {
				done.gen = gen
				return done
			}
			return msg
		}
	}
	return nil
}

func (m *Model) agentPaused() bool {
	return m.StatusBar != nil && m.StatusBar.Paused
}

func (m *Model) noteBackground(text string) {
	if text == "" || m.HistoryView == nil {
		return
	}
	m.HistoryView.AppendTurn(core.RenderedTurn{Role: "system", Content: text})
}

func (m *Model) popFollowUp(allowUser bool) (string, bool) {
	if allowUser {
		if next, ok := m.InputArea.Dequeue(); ok {
			return next, true
		}
	}
	if len(m.pendingBash) == 0 {
		return "", false
	}
	next := m.pendingBash[0]
	m.pendingBash = m.pendingBash[1:]
	return next, true
}

func (m *Model) handleBuiltinSlash(input string) (tea.Cmd, bool) {
	in := strings.TrimSpace(input)
	if in == "/planmode" {
		m.togglePlanMode()
		return nil, true
	}
	if strings.HasPrefix(in, "/plan") {
		rest := strings.TrimSpace(strings.TrimPrefix(in, "/plan"))
		if rest == "" {
			m.appendLocalInfo("(/plan) usage: /plan path/to/PLAN.md [topic or instructions]")
			return nil, true
		}
		parts := strings.SplitN(rest, " ", 2)
		path := strings.TrimSpace(parts[0])
		detail := ""
		if len(parts) > 1 {
			detail = strings.TrimSpace(parts[1])
		}
		var prompt string
		switch {
		case m.planOn && detail == "":
			prompt = fmt.Sprintf("Write a clear implementation plan for %q. Plan mode is on, so do not call write_file, edit_file, or bash. Put the plan in your reply. write_plan may update PLAN.md.", path)
		case m.planOn:
			prompt = fmt.Sprintf("Write a clear implementation plan for %q that addresses: %s. Plan mode is on, so do not call write_file, edit_file, or bash. Put the plan in your reply. write_plan may update PLAN.md.", path, detail)
		case detail == "":
			prompt = fmt.Sprintf("Create a clear implementation or design plan as Markdown in the file %q using write_file (create parent directories if needed). Include phases, concrete deliverables, and success criteria. When done, briefly confirm the path.", path)
		default:
			prompt = fmt.Sprintf("Create or update the Markdown file %q using write_file (create parent directories if needed) with a plan that addresses: %s\n\nBe specific and actionable. When done, briefly confirm the path.", path, detail)
		}
		m.HistoryView.AppendTurn(core.RenderedTurn{Role: "user", Content: in})
		m.lastError = ""
		m.agentBusy = true
		m.Live.Streaming = true
		m.scrollToBottom()
		if m.runFunc == nil {
			m.agentBusy = false
			m.Live.Streaming = false
			m.lastError = "agent not wired"
			return nil, true
		}
		return m.runFunc(prompt), true
	}

	if strings.HasPrefix(in, "/pause") {
		if m.agentBusy && m.runCancel != nil {
			rest := strings.TrimSpace(strings.TrimPrefix(in, "/pause"))
			m.lastPauseReason = rest
			m.runCancel()
			m.runCancel = nil
			m.lastError = "Agent paused by user."
		} else if m.agentBusy {
			m.lastError = "No active run to pause."
		} else {
			m.lastError = "Agent is not currently running."
		}
		return nil, true
	}

	switch in {
	case "/quit", "/exit":
		return tea.Quit, true
	case "/clear", "/reset":
		m.clearConversation()
		return nil, true
	case "/tokens":
		m.appendLocalInfo(m.tokensInfoText())
		return nil, true
	case "/model":
		m.appendLocalInfo(m.modelInfoText())
		return nil, true
	case "/compact":
		if m.compactFn == nil {
			m.lastError = "compaction is not available"
			return nil, true
		}
		m.lastError = ""
		m.agentBusy = true

		// dcode-005: dual-state sync for compaction busy state
		if m.StatusBar != nil {
			m.StatusBar.AgentBusy = true
		}

		return tea.Batch(runCompact(m.compactFn), m.armEventWait()), true
	case "/resume":
		if !m.agentBusy {
			// Resume by continuing the agentic loop with the current conversation state.
			// We append a subtle system note and trigger a continuation run (no new user input).
			m.HistoryView.AppendTurn(core.RenderedTurn{
				Role:    "system",
				Content: "(/resume) Continuing from paused state...",
			})
			m.HistoryView.GotoBottom()
			m.agentBusy = true
			m.Live.Streaming = true
			m.lastPauseReason = "" // clear paused state on resume
			if m.StatusBar != nil {
				m.StatusBar.AgentBusy = true
				m.StatusBar.Paused = false
				m.StatusBar.PausedReason = ""
			}
			if m.Live != nil {
				m.Live.Streaming = true
			}
			m.scrollToBottom()

			// Use a special continuation that does not append a new user message.
			if m.runFunc != nil {
				// The runFunc currently expects a user input string.
				// For resume we pass a marker that the runner can detect to skip user append.
				return m.runFunc("__internal_resume__"), true
			}
			m.agentBusy = false
			m.Live.Streaming = false
		} else {
			m.lastError = "Agent is already running."
		}
		return nil, true
	case "/theme":
		rest := strings.TrimSpace(strings.TrimPrefix(in, "/theme"))
		if rest == "" {
			m.appendLocalInfo(fmt.Sprintf("Current: palette=%s mode=%s (use /theme <palette> <mode> or Ctrl+K → Theme)", styles.CurrentTheme.Name, CurrentThemeMode))
			return nil, true
		}
		parts := strings.Fields(strings.ToLower(rest))
		palette := ""
		mode := styles.ThemeMode("")
		for _, p := range parts {
			switch p {
			case "light", "dark", "auto":
				mode = styles.ThemeMode(p)
			default:
				// treat as palette name (default or high-contrast etc)
				if _, ok := styles.NamedPalettes[p]; ok {
					palette = p
				} else if p == "high" || p == "contrast" || p == "high-contrast" {
					palette = "high-contrast"
				} else {
					palette = p // will be normalized by SetNamedPalette
				}
			}
		}
		if palette == "" && mode == "" {
			m.appendLocalInfo("Usage: /theme [palette] [light|dark|auto]  •  e.g. /theme high-contrast dark")
			return nil, true
		}
		if palette == "" {
			palette = styles.CurrentTheme.Name
		}
		if mode == "" {
			mode = styles.CurrentTheme.Mode
			if mode == "" {
				mode = styles.ThemeAuto
			}
		}
		styles.SetNamedPalette(palette, mode)
		m.appendLocalInfo(fmt.Sprintf("Theme: %s (%s)", palette, mode))
		m.relayout()
		if m.themeSaveFn != nil {
			_ = m.themeSaveFn(string(mode), palette)
		}
		return nil, true
	}
	return nil, false
}

func (m *Model) appendLocalInfo(text string) {
	m.HistoryView.AppendTurn(core.RenderedTurn{Role: "user", Content: text})
	m.HistoryView.GotoBottom()
}

func (m *Model) tokensInfoText() string {
	var b strings.Builder
	b.WriteString("(/tokens)\n")
	fmt.Fprintf(&b, "Model: %s\n", m.modelName)
	if m.convoMgr != nil {
		div := m.convoMgr.CharsPerToken()
		sys, msgs, lastU := m.convoMgr.ContextBreakdown()
		lastText, lastToolRes := m.convoMgr.LastUserContentBreakdown()
		fmt.Fprintf(&b, "Estimated context: ~%d / %d tokens (runes÷%d heuristic; not exact).\n",
			m.convoMgr.EstimatedTokens(), m.convoMgr.ContextLimit(), div)
		fmt.Fprintf(&b, "  Breakdown: ~%d system + ~%d in messages; last user turn ~%d.\n",
			sys, msgs, lastU)
		fmt.Fprintf(&b, "  Last user detail (est.): ~%d text + ~%d tool-result tokens (runes÷%d).\n",
			lastText, lastToolRes, div)
		fmt.Fprintf(&b, "  Tune with charsPerTokenEstimate / DROVER_CODE_CHARS_PER_TOKEN (lower divisor = earlier compaction).\n")
		if ema, last, n, ok := m.convoMgr.CalibrationHint(); ok {
			fmt.Fprintf(&b, "API calibration (B3): last round %.2fx · EMA %.2fx over %d stream(s) (API input vs heuristic before request).\n",
				last, ema, n)
			switch {
			case ema > 1.08:
				fmt.Fprintf(&b, "  EMA >1: API counts more than our estimate; lower charsPerToken or context limit to compact earlier.\n")
			case ema < 0.92:
				fmt.Fprintf(&b, "  EMA < 1: heuristic is pessimistic vs API; you may raise charsPerToken slightly.\n")
			}
		}
	} else {
		b.WriteString("Conversation manager not wired; context size unknown.\n")
	}
	fmt.Fprintf(&b, "Session API usage (cumulative): %d in / %d out tokens.\n",
		m.totalInputTokens, m.totalOutputTokens)
	if m.lastAPICallInput > 0 || m.lastAPICallOutput > 0 {
		fmt.Fprintf(&b, "Last API call (reference): %d in / %d out - compare to heuristic totals above.\n",
			m.lastAPICallInput, m.lastAPICallOutput)
	}
	fmt.Fprintf(&b, "Auto-compaction runs when the estimate exceeds the limit unless disableAutoCompaction / DROVER_CODE_DISABLE_AUTO_COMPACTION.")
	return strings.TrimSpace(b.String())
}

func (m *Model) modelInfoText() string {
	return strings.TrimSpace(fmt.Sprintf(
		"(/model) Active model: %s\nTheme: palette=%s mode=%s (Ctrl+K → Theme or /theme <palette> <mode>)\nCustom overrides: Settings.Theme.Colors in .claude/settings.json",
		m.modelName,
		styles.CurrentTheme.Name,
		CurrentThemeMode,
	))
}

func defaultSlashCommands() []slashItem {
	return []slashItem{
		{"clear", "clear conversation history"},
		{"reset", "reset conversation"},
		{"quit", "exit drover-code"},
		{"plan", "write a plan file: /plan path topic"},
		{"planmode", "toggle plan mode (also Shift+Tab)"},
		{"compact", "summarise and compress context"},
		{"model", "switch model"},
		{"tokens", "show token usage"},
		{"history", "search command history"},
		{"pause", "pause the current agent run (optionally: /pause reason)"},
		{"resume", "resume a previously paused agent run"},
		{"theme", "show or set UI theme (palette + light/dark/auto); e.g. /theme high-contrast dark"},
	}
}

func (m *Model) relayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	if m.InputArea != nil {
		m.InputArea.SetSize(m.width)
	}

	// dcode-003/004/008: keep components sized
	if m.StatusBar != nil {
		m.StatusBar.SetSize(m.width, 1)
		m.StatusBar.RiskLevel = m.GuardRiskLevel
		m.StatusBar.RiskReason = m.GuardRiskReason
		// Paused visual state (richer feedback for /pause "reason")
		m.StatusBar.Paused = (m.lastPauseReason != "" && !m.agentBusy)
		m.StatusBar.PausedReason = m.lastPauseReason
	}
	if m.Live != nil {
		m.Live.SetSize(m.width, 0)
	}
	if m.HistoryView != nil {
		hv := m.viewportHeight()
		m.HistoryView.SetSize(m.width, hv)
		m.HistoryView.MaxHistoryDisplay = m.maxHistoryDisplay
	}
	if m.InputArea != nil {
		m.InputArea.SetSize(m.width)
		// Sync paused visual state for input area banner
		m.InputArea.Paused = (m.lastPauseReason != "" && !m.agentBusy)
		m.InputArea.PausedReason = m.lastPauseReason
	}

	renderWidth := m.width - 4
	if renderWidth < 40 {
		renderWidth = 40
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(renderWidth),
	)
	if err == nil {
		m.glamourRenderer = r
	}
}

func (m *Model) viewportHeight() int {
	reserved := statusBarHeight + inputTotalHeight + 2
	if m.PermPrompt != nil || m.PermBatch != nil {
		reserved = statusBarHeight + permPromptHeight + 2
	}
	h := m.height - reserved
	if h < 4 {
		h = 4
	}
	return h
}

func (m *Model) scrollToBottom() {
	if m.HistoryView != nil {
		m.HistoryView.GotoBottom()
	}
}

func (m *Model) renderMarkdown(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = liveregion.SoftenAssistantParagraphBreaks(raw)
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		bodyW := m.width - 6
		if bodyW < 24 {
			bodyW = 24
		}
		return lipgloss.NewStyle().Width(bodyW).MarginBottom(1).Render(raw)
	}
	if m.maxGlamourRunes > 0 {
		if utf8.RuneCountInString(raw) > m.maxGlamourRunes {
			r := []rune(raw)
			snip := string(r[:m.maxGlamourRunes-1]) + "…"
			note := "\n\n_(Rich Markdown omitted: set DROVER_CODE_TUI_MAX_GLAMOUR_RUNES=0 on this turn's size.)_"
			return styleAssistantBody.Render(snip + note)
		}
	}
	if m.glamourRenderer == nil {
		return raw
	}
	out, err := m.glamourRenderer.Render(raw)
	if err != nil {
		return raw
	}
	return out
}

func (m *Model) SetRunFunc(fn RunFunc) { m.runFunc = fn }

func (m *Model) SetCompactFn(fn func() error) { m.compactFn = fn }

func (m *Model) SetThemeSaveFn(fn func(mode, palette string) error) { m.themeSaveFn = fn }

func (m *Model) SetConversation(mgr *convo.Manager) { m.convoMgr = mgr }

// SetOnClear runs after /clear resets the conversation. Use it to start a new saved session.
func (m *Model) SetOnClear(fn func()) { m.onClear = fn }

func (m *Model) clearConversation() {
	if m.HistoryView != nil {
		m.HistoryView.Clear()
	}
	m.streamBuf.Reset()
	if m.Live != nil {
		m.Live.ResetLiveStreaming()
	}
	m.lastError = ""
	if m.convoMgr != nil {
		m.convoMgr.Reset()
	}
	if m.onClear != nil {
		m.onClear()
	}
}

// SetGuardRisk allows external code (e.g. drover-guard, program.go, or event handlers)
// to push real risk signals into the TUI so the StatusBar can reflect them.
func (m *Model) SetGuardRisk(level, reason string) {
	m.GuardRiskLevel = level
	m.GuardRiskReason = reason

	if m.StatusBar != nil {
		m.StatusBar.RiskLevel = level
		m.StatusBar.RiskReason = reason
	}
}

// assessPermissionRisk provides deeper, more realistic risk heuristics than simple tool-name matching.
// It looks at tool name + input content for dangerous patterns.
func (m *Model) assessPermissionRisk(toolName string, inputJSON []byte, summary string) (level, reason string) {
	inputStr := strings.ToLower(string(inputJSON))
	summaryLower := strings.ToLower(summary)

	switch toolName {
	case "edit_file", "write_file", "multi_edit":
		// Check for high-risk files
		if strings.Contains(inputStr, ".env") ||
			strings.Contains(inputStr, "package.json") ||
			strings.Contains(inputStr, ".github/workflows") ||
			strings.Contains(inputStr, "/etc/") ||
			strings.Contains(inputStr, "dockerfile") {
			return "high", "editing sensitive configuration or build files"
		}
		return "caution", "modifying source files"

	case "bash":
		// Much better shell analysis
		dangerous := []string{
			"rm -rf", "rm -r /", "dd if=", "> /dev/", "mkfs", "format ",
			"curl | bash", "wget | bash", "sh <(", ":(){ :|:& };:", "eval $(curl",
			"shutdown", "reboot", "halt", "poweroff",
		}
		for _, pat := range dangerous {
			if strings.Contains(inputStr, pat) || strings.Contains(summaryLower, pat) {
				return "high", "potentially destructive shell command"
			}
		}
		return "caution", "executing shell command"

	case "delete_file":
		return "high", "deleting files"

	case "run_terminal_cmd", "execute_command":
		return "caution", "running terminal command"
	}

	return "normal", ""
}

// RegisterCustomCommands adds custom commands to the auto-complete list (owned by InputArea).
func (m *Model) RegisterCustomCommands(names, descs []string) {
	if m.InputArea != nil {
		m.InputArea.RegisterSlashCommands(names, descs)
	}
}

// RegisterPaletteCommands allows external code to contribute additional entries
// to the Command Palette (Ctrl+K). These commands are merged with the built-in ones.
//
// This is the primary extension point for richer integration (semantic actions
// with Category, Shortcut, RiskLevel, and direct ActionKey execution).
func (m *Model) RegisterPaletteCommands(commands []commandpalette.Command) {
	m.extraPaletteCommands = append(m.extraPaletteCommands, commands...)
}

// RegisterPaletteProvider registers a function that will be called every time
// the Command Palette is opened. The provider can return context-dependent
// or dynamically generated commands.
//
// Multiple providers can be registered; their results are appended in
// registration order.
func (m *Model) RegisterPaletteProvider(provider commandpalette.CommandProvider) {
	if provider == nil {
		return
	}
	m.paletteProviders = append(m.paletteProviders, provider)
}

// RegisterPaletteActionHandler registers a handler for a specific ActionKey.
//
// When a semantic action with that key is selected from the palette,
// the handler will be invoked. If it returns a non-nil tea.Cmd, that
// command is executed. If it returns nil (or no handler is registered),
// the default behavior applies (text injection for unknown keys).
//
// This allows external systems to own execution of their own semantic actions.
func (m *Model) RegisterPaletteActionHandler(actionKey string, handler commandpalette.ActionHandler) {
	if actionKey == "" || handler == nil {
		return
	}
	if m.paletteActionHandlers == nil {
		m.paletteActionHandlers = make(map[string]commandpalette.ActionHandler)
	}
	m.paletteActionHandlers[actionKey] = handler
}

// buildCommandPaletteCommands returns the list of commands for the palette.
// It includes all registered slash commands plus a few first-class semantic actions.
func (m *Model) buildCommandPaletteCommands() []commandpalette.Command {
	var cmds []commandpalette.Command

	// Existing slash commands come from InputArea (sole owner after consolidation)
	for _, c := range m.InputArea.Commands() {
		cmds = append(cmds, commandpalette.Command{
			Name:        c.Name,
			Description: c.Desc,
		})
	}

	// Externally registered commands (from drover, custom extensions, etc.)
	cmds = append(cmds, m.extraPaletteCommands...)

	// Dynamic providers (called every time the palette opens)
	for _, provider := range m.paletteProviders {
		if provider != nil {
			cmds = append(cmds, provider()...)
		}
	}

	// Semantic actions (direct execution) — now with categories and shortcuts for richer palette UX
	cmds = append(cmds, commandpalette.Command{
		Name:        "compact",
		Description: "Summarise and compress context (direct)",
		ActionKey:   "compact",
		Category:    "Agent",
		Shortcut:    "⌘K C",
		RiskLevel:   "normal",
	})
	cmds = append(cmds, commandpalette.Command{
		Name:        "clear",
		Description: "Clear conversation history (direct)",
		ActionKey:   "clear",
		Category:    "TUI",
		Shortcut:    "⌘K X",
		RiskLevel:   "caution",
	})
	cmds = append(cmds, commandpalette.Command{
		Name:        "tokens",
		Description: "Show detailed token usage (direct)",
		ActionKey:   "tokens",
		Category:    "TUI",
		Shortcut:    "⌘K T",
	})
	cmds = append(cmds, commandpalette.Command{
		Name:        "model",
		Description: "Show current model info (direct)",
		ActionKey:   "model",
		Category:    "TUI",
	})

	// Pause/Resume as first-class semantic actions (discoverable in Ctrl+K)
	// Conditional on state so the palette stays relevant.
	if m.agentBusy {
		cmds = append(cmds, commandpalette.Command{
			Name:        "pause",
			Description: "Pause the current agent run (Ctrl+C also works)",
			ActionKey:   "pause",
			Category:    "Agent",
			RiskLevel:   "normal",
		})
	} else if m.lastPauseReason != "" {
		// Offer resume if we recently had a pause (reason still set until resume)
		cmds = append(cmds, commandpalette.Command{
			Name:        "resume",
			Description: "Resume a paused agent run (continues from current state)",
			ActionKey:   "resume",
			Category:    "Agent",
			RiskLevel:   "normal",
		})
	}

	return cmds
}

// executePaletteAction handles semantic actions selected from the palette.
func (m *Model) executePaletteAction(key string) tea.Cmd {
	switch key {
	case "compact":
		if m.compactFn != nil {
			m.agentBusy = true
			if m.StatusBar != nil {
				m.StatusBar.AgentBusy = true
			}
			m.Live.Streaming = false
			return tea.Batch(runCompact(m.compactFn), m.armEventWait())
		}
		m.lastError = "compaction is not available"
		return nil

	case "clear", "reset":
		m.clearConversation()
		return nil

	case "tokens":
		m.appendLocalInfo(m.tokensInfoText())
		return nil

	case "model":
		m.appendLocalInfo(m.modelInfoText())
		return nil

	case "pause":
		// Semantic pause from palette (no reason; use typed /pause "reason" for details)
		if m.agentBusy && m.runCancel != nil {
			m.runCancel()
			m.runCancel = nil
			m.lastError = "Agent paused."
		}
		return nil

	case "resume":
		if !m.agentBusy {
			m.HistoryView.AppendTurn(core.RenderedTurn{
				Role:    "system",
				Content: "(/resume) Continuing from paused state...",
			})
			m.HistoryView.GotoBottom()
			m.agentBusy = true
			m.Live.Streaming = true
			m.lastPauseReason = "" // clear paused state
			if m.StatusBar != nil {
				m.StatusBar.AgentBusy = true
				m.StatusBar.Paused = false
				m.StatusBar.PausedReason = ""
			}
			if m.Live != nil {
				m.Live.Streaming = true
			}
			m.scrollToBottom()
			if m.runFunc != nil {
				return m.runFunc("__internal_resume__")
			}
			m.agentBusy = false
			m.Live.Streaming = false
		}
		return nil

	default:
		// Unknown semantic action — fall back to text injection
		m.InputArea.SetValue("/" + key + " ")
		m.InputArea.CursorEnd()
		return nil
	}
}
