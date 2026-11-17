package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/tools"
)

func TestReadOnlyRegistry(t *testing.T) {
	reg := readOnlyRegistry(t.TempDir())
	got := map[string]bool{}
	for _, def := range reg.Definitions() {
		got[def.Name] = true
		if reg.NeedsPermission(def.Name, nil) {
			t.Fatalf("%s needs permission", def.Name)
		}
	}
	want := []string{"read_file", "list_directory", "file_info", "glob", "grep"}
	if len(got) != len(want) {
		t.Fatalf("tools = %v", got)
	}
	for _, name := range want {
		if !got[name] {
			t.Fatalf("missing %s in %v", name, got)
		}
	}
	for _, banned := range []string{"explore", "bash", "write_file", "edit_file"} {
		if got[banned] {
			t.Fatalf("registered %s", banned)
		}
	}
}

func TestExplore_ReturnsMapNotFileBody(t *testing.T) {
	dir := t.TempDir()
	const body = "SECRET-FILE-BODY-SHOULD-NOT-LEAK"
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := convo.NewManagerWithSystem("parent")
	parent.Append(api.UserMessage("keep me"))

	d := &scriptedExploreDriver{}
	out, err := Explore(context.Background(), d, dir, "where is the note")
	if err != nil {
		t.Fatal(err)
	}
	if out != "note.txt — holds the note" {
		t.Fatalf("out %q", out)
	}
	if strings.Contains(out, "SECRET") {
		t.Fatal("file body returned")
	}
	if !d.sawBody {
		t.Fatal("child did not read the file")
	}
	if d.calls != 2 {
		t.Fatalf("calls %d", d.calls)
	}
	if d.bad != "" {
		t.Fatalf("child tool %s", d.bad)
	}
	if n := len(parent.Messages()); n != 1 {
		t.Fatalf("parent messages %d", n)
	}
}

func TestExplore_RejectsNilDriver(t *testing.T) {
	_, err := Explore(context.Background(), nil, t.TempDir(), "where")
	if err == nil || !strings.Contains(err.Error(), "no model") {
		t.Fatal(err)
	}
}

func TestExplore_StopsAfterTurnCap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &alwaysReadDriver{}
	_, err := Explore(context.Background(), d, dir, "read it forever")
	if err == nil || !strings.Contains(err.Error(), "stopped after 8 turns") {
		t.Fatal(err)
	}
	if d.calls != exploreMaxTurns {
		t.Fatalf("calls %d", d.calls)
	}
}

func TestLoop_MaxTurnsStopsToolLoop(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(nopTool{})
	d := &alwaysToolDriver{}
	loop := NewLoop(d, convo.NewManagerWithSystem("s"), NewDefaultToolExecutor(reg, nil, nil), reg, nil)
	loop.SetMaxTurns(2)
	err := loop.Run(context.Background(), "go")
	if err == nil || !strings.Contains(err.Error(), "stopped after 2 turns") {
		t.Fatal(err)
	}
	if d.calls != 2 || loop.LastRunTurns() != 2 {
		t.Fatalf("calls %d turns %d", d.calls, loop.LastRunTurns())
	}
}

type scriptedExploreDriver struct {
	calls   int
	sawBody bool
	bad     string
}

func (d *scriptedExploreDriver) Model() string { return "test" }

func (d *scriptedExploreDriver) Generate(_ context.Context, mgr *convo.Manager, defs []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error) {
	d.calls++
	for _, def := range defs {
		switch def.Name {
		case "explore", "bash", "write_file", "edit_file":
			d.bad = def.Name
		}
	}
	ch := make(chan SemanticEvent, 3)
	if d.calls == 1 {
		ch <- ToolCallRequested{ID: "c1", Name: "read_file", Input: json.RawMessage(`{"path":"note.txt"}`)}
	} else {
		for _, msg := range mgr.Messages() {
			for _, block := range msg.Content {
				if tr, ok := block.(api.ToolResultBlock); ok && strings.Contains(tr.Content, "SECRET-FILE-BODY-SHOULD-NOT-LEAK") {
					d.sawBody = true
				}
			}
		}
		ch <- TextYielded{Text: "note.txt — holds the note"}
	}
	ch <- TurnCompleted{}
	close(ch)
	return ch, "", nil
}

type alwaysReadDriver struct{ calls int }

func (d *alwaysReadDriver) Model() string { return "test" }

func (d *alwaysReadDriver) Generate(context.Context, *convo.Manager, []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error) {
	d.calls++
	ch := make(chan SemanticEvent, 2)
	ch <- ToolCallRequested{ID: "c", Name: "read_file", Input: json.RawMessage(`{"path":"note.txt"}`)}
	ch <- TurnCompleted{}
	close(ch)
	return ch, "", nil
}

type nopTool struct{}

func (nopTool) Name() string                         { return "nop" }
func (nopTool) Description() string                  { return "nop" }
func (nopTool) InputSchema() json.RawMessage         { return json.RawMessage(`{"type":"object"}`) }
func (nopTool) NeedsPermission(json.RawMessage) bool { return false }
func (nopTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

type alwaysToolDriver struct{ calls int }

func (d *alwaysToolDriver) Model() string { return "test" }

func (d *alwaysToolDriver) Generate(context.Context, *convo.Manager, []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error) {
	d.calls++
	ch := make(chan SemanticEvent, 2)
	ch <- ToolCallRequested{ID: "c", Name: "nop", Input: json.RawMessage(`{}`)}
	ch <- TurnCompleted{}
	close(ch)
	return ch, "", nil
}
