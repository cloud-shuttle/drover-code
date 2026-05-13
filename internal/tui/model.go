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
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/tools/fs"
	"github.com/cloudshuttle/drover-code/internal/tui/diff"
	"github.com/cloudshuttle/drover-code/internal/tui/history"
	"github.com/cloudshuttle/drover-code/internal/tui/historysearch"
)

type renderedTurn struct {
	role    string
	content string
	tools   []completedTool
}

type completedTool struct {
	name    string
	summary string
	isError bool
}

type activeTool struct {
	index   int
	name    string
	summary string
	spinner spinner.Model
}

type slashItem struct {
	name string
	desc string
}

// RunFunc submits user text to the agent loop.
type RunFunc func(input string) tea.Cmd

type Model struct {
	width, height int

	viewport    viewport.Model
	history     []renderedTurn
	viewportBuf strings.Builder

	streaming   bool
	streamBuf   strings.Builder
	streamLines string

	activeTools map[int]*activeTool
	toolOrder   []int
	pendingDone []completedTool

	messageQueue []string

	inputHistory *history.PersistentHistory
	historyIndex int
	savedInput   string

	textarea     textarea.Model
	inputFocused bool

	autoList  []slashItem
	autoIndex int
	showAuto  bool

	permPrompt *permissionPrompt
	permBatch  *permissionBatchPrompt

	diffModel   *diff.Model
	showingDiff bool

	searchModel   *historysearch.Model
	showingSearch bool

	glamourRenderer *glamour.TermRenderer

	eventCh <-chan agent.Event

	modelName         string
	workDir           string
	userName          string
	hostName          string
	totalInputTokens  int
	totalOutputTokens int
	agentBusy         bool

	lastAPICallInput  int
	lastAPICallOutput int

	lastError string

	compactionBanner string

	maxGlamourRunes   int
	maxHistoryDisplay int

	runFunc   RunFunc
	runCancel context.CancelFunc
	compactFn func() error
	convoMgr  *convo.Manager
}

// SetRunCancel allows injecting the context cancellation function for the current run
func (m *Model) SetRunCancel(cancel context.CancelFunc) {
	m.runCancel = cancel
}

func New(eventCh <-chan agent.Event, modelName, workDir, userName, hostName string) *Model {
	ta := textarea.New()
	ta.Placeholder = "Message… (Enter to send, Shift+Enter for newline)"
	ta.ShowLineNumbers = false
	ta.SetHeight(inputMinHeight)
	ta.CharLimit = 0
	ta.Focus()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()

	hist, err := history.NewPersistentHistory(workDir)
	if err != nil {
		hist = &history.PersistentHistory{}
	}

	return &Model{
		eventCh:           eventCh,
		modelName:         modelName,
		workDir:           workDir,
		userName:          userName,
		hostName:          hostName,
		activeTools:       make(map[int]*activeTool),
		inputFocused:      true,
		textarea:          ta,
		autoList:          defaultSlashCommands(),
		maxGlamourRunes:   readMaxGlamourRunesFromEnv(),
		maxHistoryDisplay: readMaxHistoryDisplayFromEnv(),
		inputHistory:      hist,
		historyIndex:      len(hist.Get()),
	}
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
		waitForEvent(m.eventCh),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		cmds = append(cmds, waitForEvent(m.eventCh))
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
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
					if m.permPrompt != nil {
						select {
						case m.permPrompt.decisionCh <- agent.PermAppliedManually:
						default:
						}
						m.permPrompt = nil
					}
				}

				m.showingDiff = false
				m.diffModel = nil
				return m, nil
			}
			return m, cmd
		}

		if m.permPrompt != nil {
			cmd := m.handlePermissionKey(msg)
			return m, cmd
		}
		if m.permBatch != nil {
			cmd := m.handlePermissionBatchKey(msg)
			return m, cmd
		}

		switch msg.Type {
		case tea.KeyCtrlC:
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
		case tea.KeyEnter:
			if msg.Alt {
				break
			}
			input := strings.TrimSpace(m.textarea.Value())
			if input != "" {
				m.inputHistory.Add(input)
				m.historyIndex = len(m.inputHistory.Get())
				m.savedInput = ""

				m.textarea.Reset()
				m.showAuto = false
				m.lastError = ""
				
				in := strings.ToLower(input)
				if in == "/quit" || in == "/exit" {
					return m, tea.Quit
				}
				if in == "/history" {
					if !m.agentBusy {
						m.searchModel = historysearch.New(m.inputHistory.Get(), m.width, m.height)
						m.showingSearch = true
					}
					return m, nil
				}

				if m.agentBusy {
					m.messageQueue = append(m.messageQueue, input)
					m.rebuildViewport()
					m.scrollToBottom()
					return m, nil
				}
				return m, m.submitInput(input)
			}
			return m, nil
		case tea.KeyEsc:
			m.showAuto = false
		case tea.KeyUp:
			if m.showAuto && m.autoIndex > 0 {
				m.autoIndex--
				return m, nil
			} else if !m.showAuto && m.textarea.Line() == 0 {
				histEntries := m.inputHistory.Get()
				if len(histEntries) > 0 && m.historyIndex > 0 {
					if m.historyIndex == len(histEntries) {
						m.savedInput = m.textarea.Value()
					}
					m.historyIndex--
					m.textarea.SetValue(histEntries[m.historyIndex])
					m.textarea.CursorEnd()
					return m, nil
				}
			}
		case tea.KeyDown:
			if m.showAuto && m.autoIndex < len(m.filteredAuto())-1 {
				m.autoIndex++
				return m, nil
			} else if !m.showAuto && m.textarea.Line() == m.textarea.LineCount()-1 {
				histEntries := m.inputHistory.Get()
				if m.historyIndex < len(histEntries) {
					m.historyIndex++
					if m.historyIndex == len(histEntries) {
						m.textarea.SetValue(m.savedInput)
					} else {
						m.textarea.SetValue(histEntries[m.historyIndex])
					}
					m.textarea.CursorEnd()
					return m, nil
				}
			}
		case tea.KeyTab:
			if m.showAuto {
				filtered := m.filteredAuto()
				if len(filtered) > 0 {
					m.textarea.SetValue("/" + filtered[m.autoIndex].name + " ")
					m.showAuto = false
					m.textarea.CursorEnd()
				}
				return m, nil
			}
		}

		var taCmd tea.Cmd
		m.textarea, taCmd = m.textarea.Update(msg)
		cmds = append(cmds, taCmd)
		if v := m.textarea.Value(); v != "" {
			sanitized := v
			if strings.Contains(sanitized, "]11;rgb:") {
				sanitized = stripTerminalOSCResponses(sanitized)
			}
			if strings.Contains(sanitized, "\x1b[") || strings.Contains(sanitized, "\\[") {
				sanitized = stripCursorPositionReports(sanitized)
			}
			if strings.Contains(sanitized, "\n\\\n") || strings.HasPrefix(sanitized, "\\\n") {
				sanitized = stripStandaloneBackslashLines(sanitized)
			}
			if strings.Contains(sanitized, "/") {
				sanitized = stripBareRGBTriplets(sanitized)
			}
			if sanitized != v {
				m.textarea.SetValue(sanitized)
				m.textarea.CursorEnd()
			}
		}
		m.updateAutoComplete()
		return m, tea.Batch(cmds...)

	case historysearch.SelectedMsg:
		m.textarea.SetValue(msg.Entry)
		m.textarea.CursorEnd()
		m.showingSearch = false
		m.searchModel = nil
		return m, nil

	case historysearch.CancelMsg:
		m.showingSearch = false
		m.searchModel = nil
		return m, nil

	case agentMsg:
		cmd := m.handleAgentEvent(msg.event)
		cmds = append(cmds, waitForEvent(m.eventCh))
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case agentRunCompleteMsg:
		m.agentBusy = false
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.lastError = "Agent paused by user."
				m.history = append(m.history, renderedTurn{
					role:    "system",
					content: "(/pause) Agent interrupted. Waiting for new instructions...",
				})
			} else {
				m.lastError = msg.err.Error()
			}
		}
		
		for len(m.messageQueue) > 0 && !m.agentBusy {
			nextInput := m.messageQueue[0]
			m.messageQueue = m.messageQueue[1:]
			if cmd := m.submitInput(nextInput); cmd != nil {
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
			m.history = append(m.history, renderedTurn{
				role:    "user",
				content: "(/compact) Older turns were summarised into one context message; recent messages kept.",
			})
		}
		m.rebuildViewport()
		m.scrollToBottom()

		cmds = append(cmds, waitForEvent(m.eventCh))
		for len(m.messageQueue) > 0 && !m.agentBusy {
			nextInput := m.messageQueue[0]
			m.messageQueue = m.messageQueue[1:]
			if cmd := m.submitInput(nextInput); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		
		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		for idx, at := range m.activeTools {
			var cmd tea.Cmd
			m.activeTools[idx].spinner, cmd = at.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) handleAgentEvent(ev agent.Event) tea.Cmd {
	switch e := ev.(type) {
	case agent.TextDeltaEvent:
		m.streamBuf.WriteString(e.Text)
		m.streamLines = stripCursorPositionReports(m.streamBuf.String())
		m.streamLines = stripTerminalOSCResponses(m.streamLines)
		m.scrollToBottom()

	case agent.ToolStartEvent:
		sp := spinner.New()
		sp.Spinner = spinner.Dot
		sp.Style = styleToolPending
		at := &activeTool{
			index:   e.CallIndex,
			name:    e.Name,
			summary: e.InputSummary,
			spinner: sp,
		}
		m.activeTools[e.CallIndex] = at
		m.toolOrder = append(m.toolOrder, e.CallIndex)
		m.scrollToBottom()
		return sp.Tick

	case agent.ToolDoneEvent:
		if _, ok := m.activeTools[e.CallIndex]; ok {
			done := completedTool{
				name:    e.Name,
				summary: e.OutputSummary,
				isError: e.IsError,
			}
			m.pendingDone = append(m.pendingDone, done)
			delete(m.activeTools, e.CallIndex)
			for i, idx := range m.toolOrder {
				if idx == e.CallIndex {
					m.toolOrder = append(m.toolOrder[:i], m.toolOrder[i+1:]...)
					break
				}
			}
		}

	case agent.PermissionRequestEvent:
		if e.ToolName == "edit_file" {
			filePath, diffStr, err := fs.PreviewEdit(m.workDir, e.Input)
			if err == nil && diffStr != "" && diffStr != "no changes" {
				dm := diff.NewDiffModel(filePath, diffStr)
				m.diffModel = &dm
				m.showingDiff = true
				
				m.permPrompt = &permissionPrompt{
					toolName:   e.ToolName,
					summary:    e.Summary,
					inputJSON:  e.Input,
					decisionCh: e.DecisionCh,
				}
				m.permBatch = nil
				m.scrollToBottom()
				return nil
			}
		}

		m.permPrompt = &permissionPrompt{
			toolName:   e.ToolName,
			summary:    e.Summary,
			inputJSON:  e.Input,
			decisionCh: e.DecisionCh,
		}
		m.permBatch = nil

	case agent.PermissionBatchRequestEvent:
		m.permBatch = &permissionBatchPrompt{
			items:      e.Items,
			decisionCh: e.DecisionCh,
		}
		m.permPrompt = nil

	case agent.UsageEvent:
		m.totalInputTokens = e.TotalInputTokens
		m.totalOutputTokens = e.TotalOutputTokens
		m.lastAPICallInput = e.InputTokens
		m.lastAPICallOutput = e.OutputTokens

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
			completed := m.pendingDone
			m.pendingDone = nil
			m.history = append(m.history, renderedTurn{
				role:    "assistant",
				content: rendered,
				tools:   completed,
			})
		}

		m.streamBuf.Reset()
		m.streamLines = ""
		m.activeTools = make(map[int]*activeTool)
		m.toolOrder = nil
		m.streaming = false
		m.agentBusy = false

		if len(m.messageQueue) > 0 {
			nextInput := m.messageQueue[0]
			m.messageQueue = m.messageQueue[1:]
			return m.submitInput(nextInput)
		}

		m.rebuildViewport()
		m.scrollToBottom()

	case agent.ErrorEvent:
		m.lastError = e.Err.Error()
		m.agentBusy = false
		m.messageQueue = nil // Clear the queue on error
		m.streaming = false
		m.streamBuf.Reset()
		m.streamLines = ""
		m.compactionBanner = ""
	}

	return nil
}

func (m *Model) handlePermissionKey(msg tea.KeyMsg) tea.Cmd {
	if m.permPrompt == nil {
		return nil
	}

	switch msg.String() {
	case "y", "Y":
		m.permPrompt.respond(agent.PermAllow)
		m.permPrompt = nil
	case "a", "A":
		m.permPrompt.respond(agent.PermAlwaysAllow)
		m.permPrompt = nil
	case "n", "N", "q", tea.KeyEsc.String():
		m.permPrompt.respond(agent.PermDeny)
		m.permPrompt = nil
	}
	return nil
}

func (m *Model) handlePermissionBatchKey(msg tea.KeyMsg) tea.Cmd {
	if m.permBatch == nil {
		return nil
	}

	switch msg.String() {
	case "y", "Y":
		m.permBatch.respond(agent.PermAllow)
		m.permBatch = nil
	case "a", "A":
		m.permBatch.respond(agent.PermAlwaysAllow)
		m.permBatch = nil
	case "n", "N", "q", tea.KeyEsc.String():
		m.permBatch.respond(agent.PermDeny)
		m.permBatch = nil
	}
	return nil
}

func (m *Model) submitInput(input string) tea.Cmd {
	if cmd, handled := m.handleBuiltinSlash(input); handled {
		return cmd
	}

	m.history = append(m.history, renderedTurn{
		role:    "user",
		content: input,
	})
	m.agentBusy = true
	m.streaming = true
	m.rebuildViewport()
	m.scrollToBottom()

	if m.runFunc != nil {
		return m.runFunc(input)
	}
	return nil
}

func (m *Model) handleBuiltinSlash(input string) (tea.Cmd, bool) {
	in := strings.TrimSpace(input)
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
		if detail == "" {
			prompt = fmt.Sprintf("Create a clear implementation or design plan as Markdown in the file %q using write_file (create parent directories if needed). Include phases, concrete deliverables, and success criteria. When done, briefly confirm the path.", path)
		} else {
			prompt = fmt.Sprintf("Create or update the Markdown file %q using write_file (create parent directories if needed) with a plan that addresses: %s\n\nBe specific and actionable. When done, briefly confirm the path.", path, detail)
		}
		m.history = append(m.history, renderedTurn{role: "user", content: in})
		m.lastError = ""
		m.agentBusy = true
		m.streaming = true
		m.rebuildViewport()
		m.scrollToBottom()
		if m.runFunc == nil {
			m.agentBusy = false
			m.streaming = false
			m.lastError = "agent not wired"
			return nil, true
		}
		return m.runFunc(prompt), true
	}

	switch in {
	case "/quit", "/exit":
		return tea.Quit, true
	case "/clear", "/reset":
		m.history = nil
		m.streamBuf.Reset()
		m.streamLines = ""
		m.lastError = ""
		if m.convoMgr != nil {
			m.convoMgr.Reset()
		}
		m.rebuildViewport()
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
		return tea.Batch(runCompact(m.compactFn), waitForEvent(m.eventCh)), true
	}
	return nil, false
}

func (m *Model) appendLocalInfo(text string) {
	m.history = append(m.history, renderedTurn{role: "user", content: text})
	m.rebuildViewport()
	m.scrollToBottom()
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
		"(/model) Active model: %s\nTo change it, set ANTHROPIC_MODEL or \"model\" in .claude/settings.json and restart.",
		m.modelName,
	))
}

func (m *Model) updateAutoComplete() {
	val := m.textarea.Value()
	if strings.HasPrefix(val, "/") && !strings.Contains(val, " ") {
		m.showAuto = true
		m.autoIndex = 0
	} else {
		m.showAuto = false
	}
}

func (m *Model) filteredAuto() []slashItem {
	val := strings.TrimPrefix(m.textarea.Value(), "/")
	var out []slashItem
	for _, item := range m.autoList {
		if strings.HasPrefix(item.name, val) {
			out = append(out, item)
		}
	}
	return out
}

func defaultSlashCommands() []slashItem {
	return []slashItem{
		{"clear", "clear conversation history"},
		{"reset", "reset conversation"},
		{"quit", "exit drover-code"},
		{"plan", "write a plan file: /plan path topic"},
		{"compact", "summarise and compress context"},
		{"model", "switch model"},
		{"tokens", "show token usage"},
		{"history", "search command history"},
	}
}

func (m *Model) relayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	m.textarea.SetWidth(m.width - 4)

	vpHeight := m.viewportHeight()
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport = viewport.New(m.width, vpHeight)
	m.viewport.Style = lipgloss.NewStyle()

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

	m.rebuildViewport()
}

func (m *Model) viewportHeight() int {
	reserved := statusBarHeight + inputTotalHeight + 2
	if m.permPrompt != nil || m.permBatch != nil {
		reserved = statusBarHeight + permPromptHeight + 2
	}
	h := m.height - reserved
	if h < 4 {
		h = 4
	}
	return h
}

func (m *Model) rebuildViewport() {
	hist := m.history
	omit := 0
	if m.maxHistoryDisplay > 0 && len(hist) > m.maxHistoryDisplay {
		omit = len(hist) - m.maxHistoryDisplay
		hist = hist[len(hist)-m.maxHistoryDisplay:]
	}
	m.viewportBuf.Reset()
	if omit > 0 {
		note := fmt.Sprintf("(+%d older turns hidden from display only; full history still sent to the API.)\n\n", omit)
		m.viewportBuf.WriteString(lipgloss.NewStyle().Foreground(colSubtle).Render(note))
	}
	for i, turn := range hist {
		if i > 0 {
			m.viewportBuf.WriteByte('\n')
		}
		m.viewportBuf.WriteString(m.renderTurn(turn))
	}
	m.viewport.SetContent(m.viewportBuf.String())
}

func (m *Model) scrollToBottom() { m.viewport.GotoBottom() }

func (m *Model) renderTurn(t renderedTurn) string {
	var b strings.Builder

	switch t.role {
	case "user":
		b.WriteString(styleUserLabel.Render("you") + "\n")
		b.WriteString(styleUserBubble.Width(m.width-4).Render(t.content) + "\n")

	case "assistant":
		b.WriteString(styleAssistantLabel.Render("drover-code") + "\n")
		for _, ct := range t.tools {
			b.WriteString(renderCompletedTool(ct))
		}
		b.WriteString(styleAssistantBody.Render(t.content))
	}

	b.WriteString("\n\n")
	return b.String()
}

func renderCompletedTool(ct completedTool) string {
	icon := styleToolDone.Render("\u2713 ")
	if ct.isError {
		icon = styleToolError.Render("\u2717 ")
	}
	line := icon + styleToolName.Render(ct.name) + " " + styleToolSummary.Render(ct.summary)
	return styleToolRow.Render(line) + "\n"
}

func (m *Model) renderMarkdown(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = softenAssistantParagraphBreaks(raw)
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

func (m *Model) SetConversation(mgr *convo.Manager) { m.convoMgr = mgr }

// RegisterCustomCommands adds custom commands to the auto-complete list.
func (m *Model) RegisterCustomCommands(names, descs []string) {
	for i, name := range names {
		desc := ""
		if i < len(descs) {
			desc = descs[i]
		}
		m.autoList = append(m.autoList, slashItem{name: name, desc: desc})
	}
}

