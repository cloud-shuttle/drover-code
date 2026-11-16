package permissionprompt

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

// PermissionPrompt is the component for a single tool permission request.
type PermissionPrompt struct {
	ToolName   string
	Summary    string
	InputJSON  json.RawMessage
	DecisionCh chan<- agent.PermissionDecision
	Width      int
}

func (p *PermissionPrompt) Respond(decision agent.PermissionDecision) {
	if p.DecisionCh != nil {
		p.DecisionCh <- decision
	}
}

func (p *PermissionPrompt) View() string {
	if p.Width == 0 {
		return ""
	}

	innerW := p.Width - 6
	if innerW < 18 {
		innerW = 18
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColWarning).Bold(true).Render("⚠  Tool permission required") + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColAccent).Bold(true).Render(p.ToolName) + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColMuted).Width(innerW).Render(p.Summary) + "\n")

	if preview := jsonPreview(p.InputJSON, innerW); preview != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(styles.ColMuted).Render(preview) + "\n")
	}
	b.WriteString("\n")

	hints := []struct{ key, label string }{
		{"y", "allow once"},
		{"a", "always allow"},
		{"n", "deny"},
	}
	var parts []string
	for _, h := range hints {
		parts = append(parts, fmt.Sprintf("%s %s",
			lipgloss.NewStyle().Foreground(styles.ColBase).Background(styles.ColSurface).Bold(true).PaddingLeft(1).PaddingRight(1).Render(h.key),
			lipgloss.NewStyle().Foreground(styles.ColMuted).Render(h.label),
		))
	}
	b.WriteString(strings.Join(parts, "  "))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColWarning).
		Padding(1, 2).
		Width(p.Width - 2).
		Render(b.String())
}

// PermissionBatchPrompt is the component for batch permission review.
type PermissionBatchPrompt struct {
	Items      []agent.PermissionBatchItem
	DecisionCh chan<- agent.PermissionDecision
	Width      int
}

func (p *PermissionBatchPrompt) Respond(decision agent.PermissionDecision) {
	if p.DecisionCh != nil {
		p.DecisionCh <- decision
	}
}

func (p *PermissionBatchPrompt) View() string {
	if p.Width == 0 {
		return ""
	}

	innerW := p.Width - 6
	if innerW < 18 {
		innerW = 18
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColWarning).Bold(true).Render("⚠  Review planned tool operations") + "\n\n")

	maxItems := 8
	if len(p.Items) < maxItems {
		maxItems = len(p.Items)
	}
	for i := 0; i < maxItems; i++ {
		it := p.Items[i]
		line := fmt.Sprintf("%d) %s — %s", i+1, it.ToolName, it.Summary)
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColMuted).Width(innerW).Render(line) + "\n")
	}
	if len(p.Items) > maxItems {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColMuted).Width(innerW).Render(fmt.Sprintf("…and %d more", len(p.Items)-maxItems)) + "\n")
	}

	b.WriteString("\n")
	hints := []struct{ key, label string }{
		{"y", "allow all once"},
		{"a", "always allow all"},
		{"n", "deny all"},
	}
	var parts []string
	for _, h := range hints {
		parts = append(parts, fmt.Sprintf("%s %s",
			lipgloss.NewStyle().Foreground(styles.ColBase).Background(styles.ColSurface).Bold(true).PaddingLeft(1).PaddingRight(1).Render(h.key),
			lipgloss.NewStyle().Foreground(styles.ColMuted).Render(h.label),
		))
	}
	b.WriteString(strings.Join(parts, "  "))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColWarning).
		Padding(1, 2).
		Width(p.Width - 2).
		Render(b.String())
}

func jsonPreview(raw json.RawMessage, maxLen int) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}

	for _, key := range []string{"command", "path", "query", "pattern", "url", "content"} {
		if v, ok := m[key]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil && s != "" {
				preview := fmt.Sprintf("%s: %s", key, s)
				if len([]rune(preview)) > maxLen {
					runes := []rune(preview)
					preview = string(runes[:maxLen-1]) + "…"
				}
				return preview
			}
		}
	}
	return ""
}