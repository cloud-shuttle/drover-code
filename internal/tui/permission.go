package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/agent"
)

type permissionPrompt struct {
	toolName   string
	summary    string
	inputJSON  json.RawMessage
	decisionCh chan<- agent.PermissionDecision
}

func (p *permissionPrompt) respond(d agent.PermissionDecision) {
	p.decisionCh <- d
}

func (p *permissionPrompt) render(width int) string {
	innerW := width - 6
	if innerW < 20 {
		innerW = 20
	}

	var b strings.Builder
	b.WriteString(stylePermTitle.Render("⚠  Tool permission required") + "\n\n")
	b.WriteString(stylePermTool.Render(p.toolName) + "\n")
	b.WriteString(stylePermSummary.Width(innerW).Render(p.summary) + "\n")

	if preview := jsonPreview(p.inputJSON, innerW); preview != "" {
		b.WriteString("\n" + stylePermSummary.Render(preview) + "\n")
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
			stylePermKey.Render(h.key),
			stylePermKeyLabel.Render(h.label),
		))
	}
	b.WriteString(strings.Join(parts, "  "))

	return stylePermBox.Width(width - 2).Render(b.String())
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

type permissionBatchPrompt struct {
	items      []agent.PermissionBatchItem
	decisionCh chan<- agent.PermissionDecision
}

func (p *permissionBatchPrompt) respond(d agent.PermissionDecision) {
	p.decisionCh <- d
}

func (p *permissionBatchPrompt) render(width int) string {
	innerW := width - 6
	if innerW < 20 {
		innerW = 20
	}

	var b strings.Builder
	b.WriteString(stylePermTitle.Render("⚠  Review planned tool operations") + "\n\n")

	maxItems := 8
	if len(p.items) < maxItems {
		maxItems = len(p.items)
	}
	for i := 0; i < maxItems; i++ {
		it := p.items[i]
		line := fmt.Sprintf("%d) %s — %s", i+1, it.ToolName, it.Summary)
		b.WriteString(stylePermSummary.Width(innerW).Render(line) + "\n")
	}
	if len(p.items) > maxItems {
		b.WriteString(stylePermSummary.Width(innerW).Render(fmt.Sprintf("…and %d more", len(p.items)-maxItems)) + "\n")
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
			stylePermKey.Render(h.key),
			stylePermKeyLabel.Render(h.label),
		))
	}
	b.WriteString(strings.Join(parts, "  "))

	return stylePermBox.Width(width - 2).Render(b.String())
}

