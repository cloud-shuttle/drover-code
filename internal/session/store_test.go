package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestStore_RoundTripAndLatest(t *testing.T) {
	root := t.TempDir()
	st := Open(root)
	dirA := t.TempDir()
	dirB := t.TempDir()

	older := Transcript{
		ID:      "session-aaa",
		WorkDir: dirA,
		Updated: time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC),
		Messages: []api.Message{
			api.UserMessage("read the notes"),
			api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "done"}}),
		},
	}
	newer := Transcript{
		ID:      "session-bbb",
		WorkDir: dirA,
		Updated: time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC),
		Messages: []api.Message{
			api.UserMessage("fix the test"),
			{
				Role: api.RoleAssistant,
				Content: []api.ContentBlock{
					api.TextBlock{Text: "calling"},
					api.ToolUseBlock{ID: "tu_1", Name: "read_file", Input: json.RawMessage(`{"path":"a.go"}`)},
				},
			},
			api.ToolResultMessage([]api.ToolResultBlock{{
				ToolUseID: "tu_1",
				Content:   "package p",
				Images:    []api.ToolImage{{MIME: "image/png", Base64: "aGk="}},
			}}),
		},
	}
	other := Transcript{
		ID:       "session-ccc",
		WorkDir:  dirB,
		Updated:  time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC),
		Messages: []api.Message{api.UserMessage("other directory")},
	}
	for _, tr := range []Transcript{older, newer, other} {
		if err := st.Save(tr); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.Load(dirA, "session-bb")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "session-bbb" || len(got.Messages) != 3 {
		t.Fatalf("prefix load: %+v", got.ID)
	}
	tool, ok := got.Messages[1].Content[1].(api.ToolUseBlock)
	if !ok || tool.Name != "read_file" || compactJSON(t, tool.Input) != `{"path":"a.go"}` {
		t.Fatalf("tool use = %#v", got.Messages[1].Content[1])
	}
	result, ok := got.Messages[2].Content[0].(api.ToolResultBlock)
	if !ok || result.Content != "package p" || len(result.Images) != 1 || result.Images[0].MIME != "image/png" {
		t.Fatalf("tool result = %#v", got.Messages[2].Content[0])
	}

	latest, err := st.LoadLatest(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "session-bbb" {
		t.Fatalf("latest = %s", latest.ID)
	}
	rows, err := st.List(dirA, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "session-bbb" || rows[0].Preview != "fix the test" {
		t.Fatalf("list = %+v", rows)
	}
	again, err := st.LoadLatest(dirB)
	if err != nil || again.ID != "session-ccc" {
		t.Fatalf("other dir latest = %+v err=%v", again.ID, err)
	}

	info, err := os.Stat(filepath.Join(root, mustBucket(t, dirA), "session-bbb.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestStore_AmbiguousEmptyAndInvalid(t *testing.T) {
	st := Open(t.TempDir())
	dir := t.TempDir()
	base := Transcript{WorkDir: dir, Updated: time.Now().UTC(), Messages: []api.Message{api.UserMessage("hi")}}
	base.ID = "session-abc"
	if err := st.Save(base); err != nil {
		t.Fatal(err)
	}
	base.ID = "session-abd"
	base.Updated = base.Updated.Add(time.Minute)
	if err := st.Save(base); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load(dir, "session-ab"); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("expected ambiguous, got %v", err)
	}
	if _, err := st.Load(dir, "../secret"); err == nil {
		t.Fatal("expected invalid id to miss")
	}
	if err := st.Save(Transcript{ID: "not/a path", WorkDir: dir, Messages: []api.Message{api.UserMessage("x")}}); err == nil {
		t.Fatal("expected invalid id error")
	}

	empty := Transcript{ID: "session-empty", WorkDir: dir, Updated: time.Now().UTC().Add(time.Hour)}
	if err := st.Save(empty); err != nil {
		t.Fatal(err)
	}
	latest, err := st.LoadLatest(dir)
	if err != nil || latest.ID == "session-empty" {
		t.Fatalf("empty became latest: %s %v", latest.ID, err)
	}

	bucket := filepath.Join(st.Root, mustBucket(t, dir))
	if err := os.WriteFile(filepath.Join(bucket, "session-bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := st.List(dir, 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("corrupt file should be skipped, rows=%+v err=%v", rows, err)
	}
}

func TestCloseDangling(t *testing.T) {
	msgs := []api.Message{
		api.UserMessage("go"),
		api.AssistantMessage([]api.ContentBlock{
			api.ToolUseBlock{ID: "tu_9", Name: "bash", Input: json.RawMessage(`{}`)},
		}),
	}
	out := CloseDangling(msgs)
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	result, ok := out[2].Content[0].(api.ToolResultBlock)
	if !ok || !result.IsError || result.ToolUseID != "tu_9" {
		t.Fatalf("result = %#v", out[2].Content[0])
	}
	if len(CloseDangling(out)) != 3 {
		t.Fatal("closed transcript should stay closed")
	}
}

func TestRecorder_RotateKeepsOldFile(t *testing.T) {
	st := Open(t.TempDir())
	dir := t.TempDir()
	msgs := []api.Message{api.UserMessage("first")}
	rec := NewRecorder(st, "session-one", dir, func() []api.Message { return msgs })
	rec.Save()
	rec.Rotate()
	msgs = nil
	rec.Save()
	msgs = []api.Message{api.UserMessage("second")}
	rec.Save()

	old, err := st.Load(dir, "session-one")
	if err != nil || old.Messages[0].Content[0].(api.TextBlock).Text != "first" {
		t.Fatalf("old session: %+v err=%v", old, err)
	}
	latest, err := st.LoadLatest(dir)
	if err != nil || latest.ID == "session-one" || latest.Messages[0].Content[0].(api.TextBlock).Text != "second" {
		t.Fatalf("latest = %+v err=%v", latest, err)
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func mustBucket(t *testing.T, workDir string) string {
	t.Helper()
	abs, err := absPath(workDir)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Base((&Store{Root: "x"}).bucketDir(abs))
}
