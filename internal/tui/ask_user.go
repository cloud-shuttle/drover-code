package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
)

type askUserPrompt struct {
	Question string
	ReplyCh  chan<- agent.AskUserReply
}

func (m *Model) answerAsk() tea.Cmd {
	if m == nil || m.askUser == nil {
		return nil
	}
	text := strings.TrimSpace(m.InputArea.Value())
	if text == "" {
		m.lastError = "Type an answer, or Esc to dismiss."
		return nil
	}
	m.InputArea.Reset()
	m.lastError = ""
	select {
	case m.askUser.ReplyCh <- agent.AskUserReply{Text: text, OK: true}:
	default:
	}
	m.askUser = nil
	return nil
}

func (m *Model) dismissAsk() {
	if m == nil || m.askUser == nil {
		return
	}
	select {
	case m.askUser.ReplyCh <- agent.AskUserReply{OK: false}:
	default:
	}
	m.askUser = nil
	if m.InputArea != nil {
		m.InputArea.Reset()
	}
}

func (m *Model) clearAsk() {
	if m == nil {
		return
	}
	m.askUser = nil
}
