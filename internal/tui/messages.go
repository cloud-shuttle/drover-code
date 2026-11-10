package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
)

type agentMsg struct {
	event agent.Event
}

// BackgroundBashMsg is a background command starting or finishing.
// Output is set only when Done is true.
type BackgroundBashMsg struct {
	Running int
	Output  string
	Done    bool
}

func waitForEvent(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return agentMsg{event: agent.DoneEvent{}}
		}
		return agentMsg{event: ev}
	}
}

type agentRunCompleteMsg struct {
	err error
	gen int
}

type compactCompleteMsg struct{ err error }

func runAgent(run func() error) tea.Cmd {
	return func() tea.Msg {
		return agentRunCompleteMsg{err: run()}
	}
}

func runCompact(fn func() error) tea.Cmd {
	return func() tea.Msg {
		return compactCompleteMsg{err: fn()}
	}
}

type tickMsg struct{ index int }
