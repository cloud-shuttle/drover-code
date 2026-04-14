package convo

import (
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestCompactionCutPoint_avoidsOrphanToolResults(t *testing.T) {
	msgs := []api.Message{
		api.UserMessage("hi"),
		api.AssistantMessage([]api.ContentBlock{
			api.ToolUseBlock{ID: "toolu_1", Name: "bash", Input: []byte(`{}`)},
		}),
		api.ToolResultMessage([]api.ToolResultBlock{
			{ToolUseID: "toolu_1", Content: "out"},
		}),
		api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "done"}}),
	}
	// Naive cut for minTail=1 would keep only the last message (assistant) — OK.
	cut, err := CompactionCutPoint(msgs, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cut != 3 {
		t.Fatalf("cut: got %d want 3", cut)
	}
	// minTail=3: naive cut=1 leaves tail starting at assistant+tool — OK.
	cut, err = CompactionCutPoint(msgs, 3)
	if err != nil {
		t.Fatal(err)
	}
	if cut != 1 {
		t.Fatalf("cut: got %d want 1", cut)
	}
}

func TestCompactionCutPoint_shiftsEarlierWhenTailStartsWithToolResults(t *testing.T) {
	// 10 user filler messages + tool round + final assistant text.
	var msgs []api.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, api.UserMessage("filler"))
	}
	msgs = append(msgs,
		api.AssistantMessage([]api.ContentBlock{
			api.ToolUseBlock{ID: "tu_x", Name: "grep", Input: []byte(`{}`)},
		}),
		api.ToolResultMessage([]api.ToolResultBlock{
			{ToolUseID: "tu_x", Content: "matches"},
		}),
		api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "summary for you"}}),
	)
	// len=13, minTail=8 => naive cut=5. msgs[5] is still "filler" user — OK.
	cut, err := CompactionCutPoint(msgs, 8)
	if err != nil {
		t.Fatal(err)
	}
	if cut != 5 {
		t.Fatalf("cut: got %d want 5", cut)
	}
	// minTail=2 => naive cut=11, tail[0] is tool-result user — must move to 10.
	cut, err = CompactionCutPoint(msgs, 2)
	if err != nil {
		t.Fatal(err)
	}
	if cut != 10 {
		t.Fatalf("cut: got %d want 10", cut)
	}
	if tailStartsWithOrphanToolResults(msgs[cut:]) {
		t.Fatal("tail should not start with orphan tool results")
	}
}

func TestCompactionCutPoint_impossibleReturnsError(t *testing.T) {
	msgs := []api.Message{
		api.UserMessage("first"),
		api.ToolResultMessage([]api.ToolResultBlock{
			{ToolUseID: "orphan", Content: "x"},
		}),
	}
	_, err := CompactionCutPoint(msgs, 1)
	if err == nil {
		t.Fatal("expected error")
	}
}
