package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/tui/core"
)

// turnsFromMessages builds the scrollback for a resumed session. Tool results
// attach to the assistant turn that requested them. Display text is clipped;
// the model still receives the full messages from the conversation manager.
func turnsFromMessages(msgs []api.Message) []core.RenderedTurn {
	var turns []core.RenderedTurn
	for _, msg := range msgs {
		if msg.Role == api.RoleAssistant {
			var text strings.Builder
			var tools []core.CompletedTool
			for _, block := range msg.Content {
				switch b := block.(type) {
				case api.TextBlock:
					if text.Len() > 0 {
						text.WriteByte('\n')
					}
					text.WriteString(b.Text)
				case api.ToolUseBlock:
					tools = append(tools, core.CompletedTool{Name: b.Name})
				}
			}
			turns = append(turns, core.RenderedTurn{
				Role:    "assistant",
				Content: text.String(),
				Tools:   tools,
			})
			continue
		}
		var text strings.Builder
		var results []api.ToolResultBlock
		for _, block := range msg.Content {
			switch b := block.(type) {
			case api.TextBlock:
				if text.Len() > 0 {
					text.WriteByte('\n')
				}
				text.WriteString(b.Text)
			case api.ToolResultBlock:
				results = append(results, b)
			}
		}
		if text.Len() > 0 {
			turns = append(turns, core.RenderedTurn{Role: "user", Content: text.String()})
		}
		if len(results) == 0 || len(turns) == 0 || turns[len(turns)-1].Role != "assistant" {
			continue
		}
		last := &turns[len(turns)-1]
		for i, result := range results {
			summary := clipDisplay(result.Content, 120)
			if len(result.Images) > 0 {
				if summary != "" {
					summary += " "
				}
				summary += "(image)"
			}
			if i < len(last.Tools) {
				last.Tools[i].Summary = summary
				last.Tools[i].IsError = result.IsError
				continue
			}
			last.Tools = append(last.Tools, core.CompletedTool{
				Name:    "tool",
				Summary: summary,
				IsError: result.IsError,
			})
		}
	}
	return turns
}

func clipDisplay(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
