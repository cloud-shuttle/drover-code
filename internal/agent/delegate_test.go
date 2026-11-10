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
	shellpkg "github.com/cloudshuttle/drover-code/internal/tools/shell"
)

func TestDelegateRegistry_CanEditAndCannotNest(t *testing.T) {
	reg := delegateRegistry(t.TempDir(), DelegateOpts{Sandbox: true, AllowBackground: true, Gopls: true})
	got := map[string]bool{}
	for _, def := range reg.Definitions() {
		got[def.Name] = true
	}
	for _, name := range []string{"read_file", "write_file", "edit_file", "bash", "glob", "grep", "git_status", "web_fetch"} {
		if !got[name] {
			t.Fatalf("missing %s in %v", name, got)
		}
	}
	for _, banned := range []string{"delegate", "explore", "ask_user", "browser", "ukc_create"} {
		if got[banned] {
			t.Fatalf("registered %s", banned)
		}
	}
	bash := reg.Get("bash").(*shellpkg.Bash)
	if !bash.Sandbox || !bash.AllowBackground {
		t.Fatalf("bash flags sandbox=%v background=%v", bash.Sandbox, bash.AllowBackground)
	}
	plain := delegateRegistry(t.TempDir(), DelegateOpts{})
	if plain.Get("bash").(*shellpkg.Bash).AllowBackground {
		t.Fatal("background defaults on")
	}
	if plain.Get("git_commit") != nil || plain.Get("git_push") != nil {
		t.Fatal("child can commit or push")
	}
}

func TestDelegate_WritesFileAndHidesToolDump(t *testing.T) {
	dir := t.TempDir()
	parent := convo.NewManagerWithSystem("parent")
	parent.Append(api.UserMessage("keep me"))
	d := &scriptedDelegateDriver{}
	out, err := Delegate(context.Background(), d, dir, "write a note", DelegateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if out != "wrote note.txt" {
		t.Fatalf("out %q", out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil || string(body) != "hello\n" {
		t.Fatalf("file %q %v", body, err)
	}
	if d.calls != 2 {
		t.Fatalf("calls %d", d.calls)
	}
	if n := len(parent.Messages()); n != 1 {
		t.Fatalf("parent messages %d", n)
	}
}

func TestDelegate_RejectsBackgroundBash(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	d := &backgroundRejectDriver{}
	out, err := Delegate(context.Background(), d, dir, "run it", DelegateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if out != "stayed stopped" {
		t.Fatalf("out %q", out)
	}
	if !d.sawRejection {
		t.Fatal("model did not see the background rejection")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("background command ran")
	}
}

func TestDelegate_StopsAfterTurnCap(t *testing.T) {
	d := &alwaysWriteDriver{}
	_, err := Delegate(context.Background(), d, t.TempDir(), "loop", DelegateOpts{})
	if err == nil || !strings.Contains(err.Error(), "stopped after 16 turns") {
		t.Fatal(err)
	}
	if d.calls != delegateMaxTurns {
		t.Fatalf("calls %d", d.calls)
	}
}

type backgroundRejectDriver struct {
	calls        int
	sawRejection bool
}

func (d *backgroundRejectDriver) Model() string { return "test" }

func (d *backgroundRejectDriver) Generate(_ context.Context, mgr *convo.Manager, _ []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error) {
	d.calls++
	ch := make(chan SemanticEvent, 3)
	if d.calls == 1 {
		ch <- ToolCallRequested{ID: "b", Name: "bash", Input: json.RawMessage(`{"command":"touch ran","background":true}`)}
	} else {
		d.sawRejection = strings.Contains(convoText(mgr), "local session")
		ch <- TextYielded{Text: "stayed stopped"}
	}
	ch <- TurnCompleted{}
	close(ch)
	return ch, "", nil
}

func convoText(mgr *convo.Manager) string {
	var b strings.Builder
	for _, msg := range mgr.Messages() {
		for _, block := range msg.Content {
			switch c := block.(type) {
			case api.TextBlock:
				b.WriteString(c.Text)
			case api.ToolResultBlock:
				b.WriteString(c.Content)
			}
		}
	}
	return b.String()
}

type scriptedDelegateDriver struct{ calls int }

func (d *scriptedDelegateDriver) Model() string { return "test" }

func (d *scriptedDelegateDriver) Generate(_ context.Context, _ *convo.Manager, defs []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error) {
	d.calls++
	for _, def := range defs {
		if def.Name == "delegate" || def.Name == "explore" || def.Name == "browser" {
			return nil, "", errNested(def.Name)
		}
	}
	ch := make(chan SemanticEvent, 3)
	if d.calls == 1 {
		ch <- ToolCallRequested{ID: "c1", Name: "write_file", Input: json.RawMessage(`{"path":"note.txt","content":"hello\n"}`)}
	} else {
		ch <- TextYielded{Text: "wrote note.txt"}
	}
	ch <- TurnCompleted{}
	close(ch)
	return ch, "", nil
}

type alwaysWriteDriver struct{ calls int }

func (d *alwaysWriteDriver) Model() string { return "test" }

func (d *alwaysWriteDriver) Generate(context.Context, *convo.Manager, []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error) {
	d.calls++
	ch := make(chan SemanticEvent, 2)
	ch <- ToolCallRequested{ID: "c", Name: "write_file", Input: json.RawMessage(`{"path":"n.txt","content":"x"}`)}
	ch <- TurnCompleted{}
	close(ch)
	return ch, "", nil
}

type nestedErr string

func (e nestedErr) Error() string { return "nested " + string(e) }

func errNested(name string) error { return nestedErr(name) }
