package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/session"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/tools"
)

type scriptedDriver struct {
	text string
}

func (d scriptedDriver) Model() string { return "test" }

func (d scriptedDriver) Generate(context.Context, *convo.Manager, []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error) {
	ch := make(chan SemanticEvent, 2)
	ch <- TextYielded{Text: d.text}
	ch <- TurnCompleted{}
	close(ch)
	return ch, "", nil
}

func TestLoop_PersistAfterAppend(t *testing.T) {
	mgr := convo.NewManagerWithSystem("sys")
	events := make(chan Event, 8)
	loop := NewLoop(scriptedDriver{text: "pong"}, mgr, nil, tools.NewRegistry(), events)
	var n atomic.Int32
	loop.SetPersist(func() { n.Add(1) })

	if err := loop.Run(context.Background(), "ping"); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("persist calls = %d, want 2", n.Load())
	}
	if got := len(mgr.Messages()); got != 2 {
		t.Fatalf("messages = %d", got)
	}
}

func TestLoop_RunMergesTrailingUser(t *testing.T) {
	mgr := convo.NewManager()
	mgr.Append(api.UserMessage("earlier"))
	events := make(chan Event, 8)
	loop := NewLoop(scriptedDriver{text: "ok"}, mgr, nil, tools.NewRegistry(), events)
	if err := loop.Run(context.Background(), "follow up"); err != nil {
		t.Fatal(err)
	}
	msgs := mgr.Messages()
	if len(msgs) != 2 || adjacentUser(msgs) {
		t.Fatalf("roles = %v", messageRoles(msgs))
	}
	tb, ok := msgs[0].Content[0].(api.TextBlock)
	if !ok || tb.Text != "earlier\nfollow up" {
		t.Fatalf("text = %#v", msgs[0].Content)
	}
}

func TestLoop_RunMergesDanglingToolResult(t *testing.T) {
	src := convo.NewManager()
	src.Append(api.UserMessage("run it"))
	src.Append(api.AssistantMessage([]api.ContentBlock{
		api.ToolUseBlock{ID: "t1", Name: "bash", Input: []byte(`{}`)},
	}))
	mgr := convo.NewManager()
	for _, msg := range session.CloseDangling(src.Messages()) {
		mgr.Append(msg)
	}
	events := make(chan Event, 8)
	loop := NewLoop(scriptedDriver{text: "continued"}, mgr, nil, tools.NewRegistry(), events)
	if err := loop.Run(context.Background(), "continue please"); err != nil {
		t.Fatal(err)
	}
	msgs := mgr.Messages()
	if len(msgs) != 4 || adjacentUser(msgs) {
		t.Fatalf("roles = %v", messageRoles(msgs))
	}
	merged := msgs[2]
	if merged.Role != api.RoleUser || len(merged.Content) != 2 {
		t.Fatalf("merged = %#v", merged)
	}
	if _, ok := merged.Content[0].(api.ToolResultBlock); !ok {
		t.Fatalf("first block = %T", merged.Content[0])
	}
	tb, ok := merged.Content[1].(api.TextBlock)
	if !ok || tb.Text != "continue please" {
		t.Fatalf("text = %#v", merged.Content[1])
	}
}

func adjacentUser(msgs []api.Message) bool {
	for i := 1; i < len(msgs); i++ {
		if msgs[i-1].Role == api.RoleUser && msgs[i].Role == api.RoleUser {
			return true
		}
	}
	return false
}

func messageRoles(msgs []api.Message) []api.Role {
	out := make([]api.Role, len(msgs))
	for i, msg := range msgs {
		out[i] = msg.Role
	}
	return out
}
