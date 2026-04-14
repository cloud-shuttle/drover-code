package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/agent"
)

// Fuzz input: byte 0 + second byte = KeyRunes; any other leading byte = KeyEsc (deny).
func FuzzModel_permissionPromptKeys(f *testing.F) {
	for _, s := range [][]byte{{0, 'y'}, {0, 'Y'}, {0, 'n'}, {0, 'q'}, {0, 'x'}, {0, 0}, {0, 255}, {1}, {2}} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		var key tea.KeyMsg
		if data[0] == 0 {
			if len(data) < 2 {
				return
			}
			key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(data[1])}}
		} else {
			key = tea.KeyMsg{Type: tea.KeyEsc}
		}

		ch := make(chan agent.Event, 1)
		m := New(ch, "m", "/w", "u", "h")
		dec := make(chan agent.PermissionDecision, 1)
		_, _ = m.Update(agentMsg{event: agent.PermissionRequestEvent{
			ToolName:   "bash",
			Summary:    "s",
			Input:      []byte(`{}`),
			DecisionCh: dec,
		}})
		next, _ := m.Update(key)
		m2 := next.(*Model)

		if data[0] != 0 {
			if m2.permPrompt != nil {
				t.Fatal("esc should clear prompt")
			}
			if d := <-dec; d != agent.PermDeny {
				t.Fatalf("esc deny: got %v", d)
			}
			return
		}

		b := data[1]
		isDecisionKey := b == 'y' || b == 'Y' || b == 'n' || b == 'N' || b == 'a' || b == 'A' || b == 'q'
		if isDecisionKey {
			if m2.permPrompt != nil {
				t.Fatalf("prompt should clear for key %q", b)
			}
			select {
			case d := <-dec:
				switch b {
				case 'y', 'Y':
					if d != agent.PermAllow {
						t.Fatalf("allow: got %v", d)
					}
				case 'a', 'A':
					if d != agent.PermAlwaysAllow {
						t.Fatalf("always: got %v", d)
					}
				case 'n', 'N', 'q':
					if d != agent.PermDeny {
						t.Fatalf("deny: got %v", d)
					}
				}
			default:
				t.Fatal("missing decision")
			}
			return
		}
		if m2.permPrompt == nil {
			t.Fatalf("prompt should remain for key %q", b)
		}
		select {
		case <-dec:
			t.Fatal("unexpected decision")
		default:
		}
	})
}

func FuzzModel_permissionBatchKeys(f *testing.F) {
	for _, s := range [][]byte{{0, 'y'}, {0, 'n'}, {0, 'x'}, {1}} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		var key tea.KeyMsg
		if data[0] == 0 {
			if len(data) < 2 {
				return
			}
			key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(data[1])}}
		} else {
			key = tea.KeyMsg{Type: tea.KeyEsc}
		}

		ch := make(chan agent.Event, 1)
		m := New(ch, "m", "/w", "u", "h")
		dec := make(chan agent.PermissionDecision, 1)
		_, _ = m.Update(agentMsg{event: agent.PermissionBatchRequestEvent{
			Items: []agent.PermissionBatchItem{
				{ToolName: "bash", Summary: "a", Input: []byte(`{}`)},
			},
			DecisionCh: dec,
		}})
		next, _ := m.Update(key)
		m2 := next.(*Model)

		if data[0] != 0 {
			if m2.permBatch != nil {
				t.Fatal("esc should clear batch")
			}
			if d := <-dec; d != agent.PermDeny {
				t.Fatalf("esc deny: got %v", d)
			}
			return
		}

		b := data[1]
		isDecisionKey := b == 'y' || b == 'Y' || b == 'n' || b == 'N' || b == 'a' || b == 'A' || b == 'q'
		if isDecisionKey {
			if m2.permBatch != nil {
				t.Fatalf("batch should clear for key %q", b)
			}
			select {
			case d := <-dec:
				switch b {
				case 'y', 'Y':
					if d != agent.PermAllow {
						t.Fatalf("allow: got %v", d)
					}
				case 'a', 'A':
					if d != agent.PermAlwaysAllow {
						t.Fatalf("always: got %v", d)
					}
				case 'n', 'N', 'q':
					if d != agent.PermDeny {
						t.Fatalf("deny: got %v", d)
					}
				}
			default:
				t.Fatal("missing decision")
			}
			return
		}
		if m2.permBatch == nil {
			t.Fatalf("batch prompt should remain for key %q", b)
		}
		select {
		case <-dec:
			t.Fatal("unexpected decision")
		default:
		}
	})
}
