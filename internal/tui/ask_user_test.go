package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
)

func TestAskUser_EnterReturnsAnswer(t *testing.T) {
	m := New(nil, "m", t.TempDir(), "u", "h")
	m.width = 80
	m.height = 24
	reply := make(chan agent.AskUserReply, 1)
	m.Update(agentMsg{event: agent.AskUserEvent{Question: "Ship it?", ReplyCh: reply}})
	if m.askUser == nil {
		t.Fatal("no prompt")
	}
	if !strings.Contains(m.View(), "Ship it?") {
		t.Fatalf("view %q", m.View())
	}
	m.InputArea.SetValue("yes")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	select {
	case got := <-reply:
		if !got.OK || got.Text != "yes" {
			t.Fatalf("%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no reply")
	}
	if m.askUser != nil {
		t.Fatal("still asking")
	}
}

func TestAskUser_EmptyEnterWaits(t *testing.T) {
	m := New(nil, "m", t.TempDir(), "u", "h")
	reply := make(chan agent.AskUserReply, 1)
	m.Update(agentMsg{event: agent.AskUserEvent{Question: "Name?", ReplyCh: reply}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	select {
	case got := <-reply:
		t.Fatalf("unexpected %+v", got)
	default:
	}
	if m.askUser == nil || m.lastError == "" {
		t.Fatal("prompt dropped")
	}
}

func TestAskUser_EscDismisses(t *testing.T) {
	m := New(nil, "m", t.TempDir(), "u", "h")
	reply := make(chan agent.AskUserReply, 1)
	m.Update(agentMsg{event: agent.AskUserEvent{Question: "Name?", ReplyCh: reply}})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := <-reply
	if got.OK {
		t.Fatal("dismissed reply was ok")
	}
	if m.askUser != nil {
		t.Fatal("still asking")
	}
}

func TestProgramAskUser_AnswerAndDismiss(t *testing.T) {
	p := &Program{eventCh: make(chan agent.Event, 1), ctx: context.Background()}
	go func() {
		ev := (<-p.eventCh).(agent.AskUserEvent)
		ev.ReplyCh <- agent.AskUserReply{Text: "  yes ", OK: true}
	}()
	got, err := p.AskUser(context.Background(), "Ship it?")
	if err != nil || got != "yes" {
		t.Fatalf("got %q err %v", got, err)
	}

	go func() {
		ev := (<-p.eventCh).(agent.AskUserEvent)
		ev.ReplyCh <- agent.AskUserReply{OK: false}
	}()
	_, err = p.AskUser(context.Background(), "again?")
	if err == nil || !strings.Contains(err.Error(), "dismissed") {
		t.Fatal(err)
	}
}

func TestProgramAskUser_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Program{eventCh: make(chan agent.Event, 1), ctx: context.Background()}
	go func() {
		<-p.eventCh
		cancel()
	}()
	_, err := p.AskUser(ctx, "wait")
	if err == nil {
		t.Fatal("expected cancel")
	}
}
