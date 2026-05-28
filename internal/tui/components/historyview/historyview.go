package historyview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/tui/core"
	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

var (
	styleUserLabel = lipgloss.NewStyle().
			Foreground(styles.ColAccent).
			Bold(true)

	styleUserBubble = lipgloss.NewStyle().
			Background(styles.ColUserBg).
			Foreground(styles.ColUserFg).
			PaddingLeft(2).
			PaddingRight(2).
			PaddingTop(1).
			PaddingBottom(1).
			MarginBottom(1)

	styleAssistantLabel = lipgloss.NewStyle().
				Foreground(styles.ColMuted)

	styleAssistantBody = lipgloss.NewStyle().
				MarginBottom(1)

	styleToolRow = lipgloss.NewStyle().
			PaddingLeft(2)

	styleToolName = lipgloss.NewStyle().
			Foreground(styles.ColAccent).
			Bold(true)

	styleToolSummary = lipgloss.NewStyle().
				Foreground(styles.ColMuted)

	styleToolDone = lipgloss.NewStyle().
			Foreground(styles.ColSuccess)

	styleToolError = lipgloss.NewStyle().
			Foreground(styles.ColError)

	styleSystemNote = lipgloss.NewStyle().
			Foreground(styles.ColSubtle).
			Italic(true)
)

// HistoryView owns the scrollable history viewport and the list of rendered turns.
// It handles truncation (maxHistoryDisplay), rebuilding content, and scrolling.
type HistoryView struct {
	Viewport viewport.Model
	Turns    []core.RenderedTurn

	// Config from Model (SetSize + MaxHistoryDisplay are called from relayout)
	MaxHistoryDisplay int
	Width             int
	Height            int
}

func New() *HistoryView {
	vp := viewport.New(0, 0)
	vp.Style = lipgloss.NewStyle()
	return &HistoryView{
		Viewport: vp,
	}
}

func (h *HistoryView) SetSize(w, height int) {
	h.Width = w
	h.Height = height
	h.Viewport.Width = w
	h.Viewport.Height = height
}

// SetTurns replaces the turns and triggers a rebuild of the viewport content.
func (h *HistoryView) SetTurns(turns []core.RenderedTurn) {
	h.Turns = turns
	h.rebuild()
}

// AppendTurn adds a single turn (user or assistant) and rebuilds the viewport.
// This is the primary mutation API when HistoryView is the source of truth.
func (h *HistoryView) AppendTurn(turn core.RenderedTurn) {
	h.Turns = append(h.Turns, turn)
	h.rebuild()
}

// Clear removes all turns (used by /clear and /reset).
func (h *HistoryView) Clear() {
	h.Turns = nil
	h.Viewport.SetContent("")
}

// Len returns the number of turns currently stored.
func (h *HistoryView) Len() int {
	return len(h.Turns)
}

// GetTurns returns a copy of the current turns (safe for test assertions).
func (h *HistoryView) GetTurns() []core.RenderedTurn {
	out := make([]core.RenderedTurn, len(h.Turns))
	copy(out, h.Turns)
	return out
}

// SetMaxHistoryDisplay updates the truncation limit and rebuilds if needed.
func (h *HistoryView) SetMaxHistoryDisplay(n int) {
	if h.MaxHistoryDisplay != n {
		h.MaxHistoryDisplay = n
		h.rebuild()
	}
}

// rebuild rebuilds the viewport content from Turns, applying maxHistoryDisplay truncation.
func (h *HistoryView) rebuild() {
	hist := h.Turns
	omit := 0
	if h.MaxHistoryDisplay > 0 && len(hist) > h.MaxHistoryDisplay {
		omit = len(hist) - h.MaxHistoryDisplay
		hist = hist[len(hist)-h.MaxHistoryDisplay:]
	}

	var buf strings.Builder
	if omit > 0 {
		note := fmt.Sprintf("(+%d older turns hidden from display only; full history still sent to the API.)\n\n", omit)
		buf.WriteString(lipgloss.NewStyle().Foreground(styles.ColSubtle).Render(note))
	}

	for i, turn := range hist {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(h.renderTurn(turn))
	}

	h.Viewport.SetContent(buf.String())
}

func (h *HistoryView) renderTurn(t core.RenderedTurn) string {
	var b strings.Builder

	switch t.Role {
	case "user":
		b.WriteString(styleUserLabel.Render("you") + "\n")
		b.WriteString(styleUserBubble.Width(h.Width-4).Render(t.Content) + "\n")

	case "assistant":
		b.WriteString(styleAssistantLabel.Render("drover-code") + "\n")
		for _, ct := range t.Tools {
			b.WriteString(h.renderCompletedTool(ct))
		}
		b.WriteString(styleAssistantBody.Render(t.Content))

	case "system":
		// Render system notes (e.g. pause/compact banners injected into history) subtly.
		b.WriteString(styleSystemNote.Render(t.Content))
	default:
		// Unknown role: render content plainly (defensive).
		b.WriteString(t.Content)
	}

	b.WriteString("\n\n")
	return b.String()
}

func (h *HistoryView) renderCompletedTool(ct core.CompletedTool) string {
	icon := styleToolDone.Render("\u2713 ")
	if ct.IsError {
		icon = styleToolError.Render("\u2717 ")
	}
	line := icon + styleToolName.Render(ct.Name) + " " + styleToolSummary.Render(ct.Summary)
	return styleToolRow.Render(line) + "\n"
}

func (h *HistoryView) View() string {
	return h.Viewport.View()
}

func (h *HistoryView) GotoBottom() {
	h.Viewport.GotoBottom()
}

// Update forwards messages (e.g. PgUp/PgDown, mouse) to the viewport.
// Returns the (possibly updated) HistoryView to match component conventions in this codebase.
func (h *HistoryView) Update(msg tea.Msg) (*HistoryView, tea.Cmd) {
	var cmd tea.Cmd
	h.Viewport, cmd = h.Viewport.Update(msg)
	return h, cmd
}