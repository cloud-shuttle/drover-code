package tui

import (
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestTurnsFromMessages(t *testing.T) {
	msgs := []api.Message{
		api.UserMessage("look at main"),
		api.AssistantMessage([]api.ContentBlock{
			api.TextBlock{Text: "reading"},
			api.ToolUseBlock{ID: "tu_1", Name: "read_file"},
		}),
		api.ToolResultMessage([]api.ToolResultBlock{{
			ToolUseID: "tu_1",
			Content:   "package main",
		}}),
	}
	turns := turnsFromMessages(msgs)
	if len(turns) != 2 {
		t.Fatalf("turns = %d", len(turns))
	}
	if turns[0].Role != "user" || turns[0].Content != "look at main" {
		t.Fatalf("user = %+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Content != "reading" {
		t.Fatalf("assistant = %+v", turns[1])
	}
	if len(turns[1].Tools) != 1 || turns[1].Tools[0].Name != "read_file" || turns[1].Tools[0].Summary != "package main" {
		t.Fatalf("tools = %+v", turns[1].Tools)
	}
}
