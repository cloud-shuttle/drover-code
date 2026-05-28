package core

import tea "github.com/charmbracelet/bubbletea"

// Component is a lightweight interface for reusable TUI pieces.
// We keep it optional and minimal to avoid over-abstraction early.
type Component interface {
	View() string
	Update(tea.Msg) (Component, tea.Cmd)
	SetSize(width, height int)
}

// RenderedTurn represents a completed turn in the conversation history.
// This is the public version that components can use.
type RenderedTurn struct {
	Role    string
	Content string
	Tools   []CompletedTool
}

// CompletedTool is metadata about a tool call that has finished.
type CompletedTool struct {
	Name    string
	Summary string
	IsError bool
}