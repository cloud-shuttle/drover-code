package inputarea

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

// Suggestion represents one autocomplete entry (name + description).
type Suggestion struct {
	Name string
	Desc string
}

// InputArea owns the bottom input region: the styled textarea, the "N messages queued" banner,
// the registered slash commands, and the autocomplete state.
//
// After the dcode-009 consolidation, this is the sole source of truth for all input-related state.
type InputArea struct {
	Textarea textarea.Model

	MessageQueue []string

	// Registered slash commands (used for autocomplete + command palette)
	commands []slashCommand

	// Autocomplete state (owned here, not synced from Model)
	showAuto    bool
	autoIndex   int
	suggestions []Suggestion

	Width        int
	inputFocused bool
}

// slashCommand is the internal representation of a registered /command.
type slashCommand struct {
	name string
	desc string
}

// New creates a fresh InputArea with a configured textarea.
func New() *InputArea {
	ta := textarea.New()
	ta.Placeholder = "Message… (Enter to send, Shift+Enter for newline)"
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.CharLimit = 0
	ta.Focus()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()

	return &InputArea{
		Textarea:     ta,
		inputFocused: true,
	}
}

// RegisterSlashCommands replaces the list of available /commands used for autocomplete
// and for populating the Command Palette.
func (ia *InputArea) RegisterSlashCommands(names, descs []string) {
	ia.commands = nil
	for i, name := range names {
		desc := ""
		if i < len(descs) {
			desc = descs[i]
		}
		ia.commands = append(ia.commands, slashCommand{name: name, desc: desc})
	}
}

// Commands returns a copy of the registered slash commands (for Command Palette).
func (ia *InputArea) Commands() []struct{ Name, Desc string } {
	out := make([]struct{ Name, Desc string }, len(ia.commands))
	for i, c := range ia.commands {
		out[i] = struct{ Name, Desc string }{Name: c.name, Desc: c.desc}
	}
	return out
}

// SetSize updates the available width. Height is driven by the textarea's own height + banners.
func (ia *InputArea) SetSize(w int) {
	ia.Width = w
	ia.Textarea.SetWidth(w - 4)
}

// SetFocus controls the border style (focused vs blurred).
func (ia *InputArea) SetFocus(focused bool) {
	ia.inputFocused = focused
	if focused {
		ia.Textarea.Focus()
	} else {
		ia.Textarea.Blur()
	}
}

// SetMessageQueue replaces the queued messages (used for the banner when busy).
func (ia *InputArea) SetMessageQueue(q []string) {
	ia.MessageQueue = q
}

// Queue appends a message to the queue (used when agent is busy).
func (ia *InputArea) Queue(input string) {
	ia.MessageQueue = append(ia.MessageQueue, input)
}

// DrainQueue returns and clears the current queue.
func (ia *InputArea) DrainQueue() []string {
	q := ia.MessageQueue
	ia.MessageQueue = nil
	return q
}

// Dequeue removes and returns the next queued message, if any.
func (ia *InputArea) Dequeue() (string, bool) {
	if len(ia.MessageQueue) == 0 {
		return "", false
	}
	next := ia.MessageQueue[0]
	ia.MessageQueue = ia.MessageQueue[1:]
	return next, true
}

// QueuedMessages returns a copy of the current queued messages (for testing/inspection).
func (ia *InputArea) QueuedMessages() []string {
	out := make([]string, len(ia.MessageQueue))
	copy(out, ia.MessageQueue)
	return out
}

// UpdateAutocomplete examines the current textarea value and decides whether to show
// the autocomplete dropdown. Called after most key events.
func (ia *InputArea) UpdateAutocomplete() {
	val := ia.Textarea.Value()
	if strings.HasPrefix(val, "/") && !strings.Contains(val, " ") {
		ia.showAuto = true
		ia.autoIndex = 0
	} else {
		ia.showAuto = false
	}
	ia.rebuildSuggestions()
}

func (ia *InputArea) rebuildSuggestions() {
	ia.suggestions = nil
	if !ia.showAuto {
		return
	}
	prefix := strings.TrimPrefix(ia.Textarea.Value(), "/")
	for _, c := range ia.commands {
		if strings.HasPrefix(c.name, prefix) {
			ia.suggestions = append(ia.suggestions, Suggestion{Name: c.name, Desc: c.desc})
		}
	}
}

// AcceptAutocomplete commits the currently selected autocomplete item into the textarea
// and hides the dropdown. Returns true if an item was accepted.
func (ia *InputArea) AcceptAutocomplete() bool {
	if !ia.showAuto || len(ia.suggestions) == 0 {
		return false
	}
	if ia.autoIndex < 0 || ia.autoIndex >= len(ia.suggestions) {
		return false
	}
	selected := ia.suggestions[ia.autoIndex]
	ia.Textarea.SetValue("/" + selected.Name + " ")
	ia.Textarea.CursorEnd()
	ia.showAuto = false
	ia.suggestions = nil
	return true
}

// ClearAutocomplete hides the autocomplete dropdown.
func (ia *InputArea) ClearAutocomplete() {
	ia.showAuto = false
	ia.suggestions = nil
}

// AutoActive returns whether the autocomplete dropdown is currently visible.
func (ia *InputArea) AutoActive() bool { return ia.showAuto }

// AutoIndex returns the currently highlighted autocomplete item index.
func (ia *InputArea) AutoIndex() int { return ia.autoIndex }

// SetAutoState is kept for the component's own tests during the transition.
// New code should use UpdateAutocomplete / SetAutoIndex / AcceptAutocomplete.
func (ia *InputArea) SetAutoState(show bool, index int, suggestions []Suggestion) {
	ia.showAuto = show
	ia.autoIndex = index
	ia.suggestions = suggestions
}

// SetAutoIndex sets the highlighted index (used for arrow navigation).
func (ia *InputArea) SetAutoIndex(i int) {
	if i < 0 {
		i = 0
	}
	if len(ia.suggestions) > 0 && i >= len(ia.suggestions) {
		i = len(ia.suggestions) - 1
	}
	ia.autoIndex = i
}

// Value returns the current text in the textarea.
func (ia *InputArea) Value() string {
	return ia.Textarea.Value()
}

// SetValue replaces the textarea content.
func (ia *InputArea) SetValue(v string) {
	ia.Textarea.SetValue(v)
}

// Reset clears the textarea.
func (ia *InputArea) Reset() {
	ia.Textarea.Reset()
}

// CursorEnd moves the cursor to the end of the current content.
func (ia *InputArea) CursorEnd() {
	ia.Textarea.CursorEnd()
}

// Update forwards a tea.Msg (primarily key and paste events) to the inner textarea and returns the updated component.
func (ia *InputArea) Update(msg tea.Msg) (*InputArea, tea.Cmd) {
	var cmd tea.Cmd
	ia.Textarea, cmd = ia.Textarea.Update(msg)
	return ia, cmd
}

// View renders the full input region (optional autocomplete dropdown + optional queue banner + bordered textarea).
func (ia *InputArea) View() string {
	if ia.Width == 0 {
		return ""
	}

	var border lipgloss.Style
	if ia.inputFocused {
		border = styleInputBorderFocused
	} else {
		border = styleInputBorder
	}

	input := border.Width(ia.Width - 2).Render(ia.Textarea.View())

	if len(ia.MessageQueue) > 0 {
		queuedText := fmt.Sprintf("⏳ %d message(s) queued...", len(ia.MessageQueue))
		queuedBanner := lipgloss.NewStyle().Foreground(lipgloss.Color("204")).MarginLeft(2).Render(queuedText)
		input = lipgloss.JoinVertical(lipgloss.Left, queuedBanner, input)
	}

	if ia.showAuto {
		auto := ia.renderAutoComplete()
		if auto != "" {
			return lipgloss.JoinVertical(lipgloss.Left, auto, input)
		}
	}
	return input
}

func (ia *InputArea) renderAutoComplete() string {
	items := ia.suggestions
	if len(items) == 0 {
		return ""
	}
	if len(items) > 6 {
		items = items[:6]
	}

	var rows []string
	for i, item := range items {
		label := "/" + item.Name
		desc := item.Desc
		var row string
		if i == ia.autoIndex {
			row = styleAutoItemSelected.Render(
				fmt.Sprintf("%-16s %s", label, desc),
			)
		} else {
			row = styleAutoItem.Render(
				fmt.Sprintf("%-16s %s", label, desc),
			)
		}
		rows = append(rows, row)
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColBorder).
		Width(ia.Width - 4).
		Render(strings.Join(rows, "\n"))

	return box
}

var (
	styleInputBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(styles.ColBorder)

	styleInputBorderFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(styles.ColAccent)

	styleAutoItem = lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(styles.ColMuted)

	styleAutoItemSelected = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(styles.ColAccent).
				Bold(true)
)