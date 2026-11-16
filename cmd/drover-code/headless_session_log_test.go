package main

import (
	"path/filepath"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/pkg/sessionevent"
)

func TestReleaseBackgroundCallsHook(t *testing.T) {
	called := false
	stopBackground = func() { called = true }
	t.Cleanup(func() { stopBackground = nil })
	releaseBackground()
	if !called {
		t.Fatal("background hook was not called")
	}
}

func TestRecordSessionObserveWritesToolAndApproval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	headlessSession = sessionLog{path: path, sessionID: "s1", issueID: "CLO-1", phase: "execute", envelope: true}
	t.Cleanup(func() { headlessSession = sessionLog{} })

	headlessSession.begin()
	headlessSession.userMessage("hello")
	recordSessionObserve(agent.ToolStartEvent{ID: "tool-1", Name: "read_file", InputSummary: "main.go"})
	recordSessionObserve(agent.ToolDoneEvent{ID: "tool-1", Name: "read_file"})
	recordSessionObserve(agent.PermissionRequestEvent{ToolName: "bash"})
	recordSessionObserve(agent.ToolStartEvent{ID: "tool-2", Name: "write_file", InputSummary: "out.txt"})

	got, err := sessionevent.ReadAll(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		sessionevent.TypeStarted,
		sessionevent.TypeLinked,
		sessionevent.TypeStatus,
		sessionevent.TypeMessage,
		sessionevent.TypeToolRequested,
		sessionevent.TypeToolFinished,
		sessionevent.TypeApprovalRequested,
		sessionevent.TypeToolRequested,
		sessionevent.TypeArtifactWritten,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i, typ := range want {
		if got[i].Type != typ {
			t.Fatalf("event %d: got %s want %s", i, got[i].Type, typ)
		}
		if got[i].V != sessionevent.Version || got[i].SessionID != "s1" {
			t.Fatalf("event %d identity: %+v", i, got[i])
		}
	}
	if got[2].Data["status"] != "running" {
		t.Fatalf("status data: %+v", got[2].Data)
	}
}

func TestSessionLogSkipsEnvelopeWhenOrchestratorOwnsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log := sessionLog{path: path, sessionID: "s1", envelope: false}
	log.begin()
	got, err := sessionevent.ReadAll(path)
	if err == nil && len(got) != 0 {
		t.Fatalf("envelope off should not write, got %+v", got)
	}
}
