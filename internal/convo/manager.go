// Package convo manages conversation state for the agent loop.
package convo

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/api"
)

const (
	DefaultCharsPerToken  = 4
	DefaultContextLimit   = 180_000
	maxCharsPerTokenClamp = 32
)

// Manager holds the conversation history and exposes a thread-safe API.
type Manager struct {
	mu            sync.RWMutex
	messages      []api.Message
	systemPrompt  string
	contextLimit  int
	charsPerToken int // runes per estimated token; 0 means DefaultCharsPerToken

	// API calibration (see RecordAPICalibration).
	calibEMA     float64
	calibSamples int
	calibLastAPI int
	calibLastEst int
}

func NewManager() *Manager {
	return &Manager{contextLimit: DefaultContextLimit}
}

func NewManagerWithSystem(systemPrompt string) *Manager {
	return &Manager{
		systemPrompt: systemPrompt,
		contextLimit: DefaultContextLimit,
	}
}

func (m *Manager) SetSystemPrompt(prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemPrompt = prompt
}

func (m *Manager) SystemPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.systemPrompt
}

func (m *Manager) Append(msg api.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

func (m *Manager) Messages() []api.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap := make([]api.Message, len(m.messages))
	copy(snap, m.messages)
	return snap
}

func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = m.messages[:0]
	m.calibEMA = 0
	m.calibSamples = 0
	m.calibLastAPI = 0
	m.calibLastEst = 0
}

// ContextLimit returns the soft token budget used by NeedsCompaction (estimated tokens, chars/4 heuristic).
func (m *Manager) ContextLimit() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.contextLimit
}

// SetContextLimit updates the soft budget for NeedsCompaction. Used for tests and tuning.
func (m *Manager) SetContextLimit(limit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit > 0 {
		m.contextLimit = limit
	}
}

// SetCharsPerToken sets runes÷N for the local token estimate (default 4).
// Values outside 1..maxCharsPerTokenClamp are ignored.
func (m *Manager) SetCharsPerToken(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n >= 1 && n <= maxCharsPerTokenClamp {
		m.charsPerToken = n
	}
}

// CharsPerToken returns the active divisor for EstimatedTokens (default 4).
func (m *Manager) CharsPerToken() int {
	m.mu.RLock()
	n := m.charsPerToken
	m.mu.RUnlock()
	if n <= 0 {
		return DefaultCharsPerToken
	}
	return n
}

func (m *Manager) divisorLocked() int {
	if m.charsPerToken <= 0 {
		return DefaultCharsPerToken
	}
	return m.charsPerToken
}

func (m *Manager) NeedsCompaction() bool {
	return m.EstimatedTokens() > m.contextLimit
}

func (m *Manager) EstimatedTokens() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	div := m.divisorLocked()
	total := utf8.RuneCountInString(m.systemPrompt)
	for _, msg := range m.messages {
		total += contentChars(msg.Content)
	}
	return total / div
}

// ContextBreakdown returns estimated token counts (char÷4 heuristic): system
// prompt only, all messages (user+assistant+tool), and the last user turn only
// (often dominated by tool results).
func (m *Manager) ContextBreakdown() (systemTok, messagesTok, lastUserTok int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sysRunes := utf8.RuneCountInString(m.systemPrompt)
	var msgRunes int
	var lastUserRunes int
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == api.RoleUser {
			lastUserRunes = contentChars(m.messages[i].Content)
			break
		}
	}
	div := m.divisorLocked()
	for _, msg := range m.messages {
		msgRunes += contentChars(msg.Content)
	}
	return sysRunes / div, msgRunes / div, lastUserRunes / div
}

// LastUserContentBreakdown splits the last user message into estimated token
// counts for plain text vs tool-result blocks (char÷div heuristic). User turns
// that are only tool results show a large "tool results" share — typical when
// the model just received big read_file output.
func (m *Manager) LastUserContentBreakdown() (textTok, toolResultTok int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	div := m.divisorLocked()
	var textRunes, toolResRunes int
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role != api.RoleUser {
			continue
		}
		for _, block := range m.messages[i].Content {
			switch b := block.(type) {
			case api.TextBlock:
				textRunes += utf8.RuneCountInString(b.Text)
			case api.ToolResultBlock:
				toolResRunes += utf8.RuneCountInString(b.Content)
			}
		}
		break
	}
	return textRunes / div, toolResRunes / div
}

func contentChars(blocks []api.ContentBlock) int {
	var n int
	for _, block := range blocks {
		switch b := block.(type) {
		case api.TextBlock:
			n += utf8.RuneCountInString(b.Text)
		case api.ToolUseBlock:
			n += utf8.RuneCountInString(b.Name) + len(b.Input)
		case api.ToolResultBlock:
			n += utf8.RuneCountInString(b.Content)
		}
	}
	return n
}

func (m *Manager) Summarise(summary string, keepTail int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.messages) <= keepTail {
		return
	}
	cutPoint := len(m.messages) - keepTail
	tail := make([]api.Message, keepTail)
	copy(tail, m.messages[cutPoint:])

	summaryMsg := api.UserMessage(summarySentinel(summary))
	m.messages = append([]api.Message{summaryMsg}, tail...)
}

func summarySentinel(summary string) string {
	var b strings.Builder
	b.WriteString("[Earlier conversation summary — treat as background context]\n\n")
	b.WriteString(summary)
	return b.String()
}
