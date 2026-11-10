package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/tools"
)

func TestShiftTabTogglesPlanMode(t *testing.T) {
	m := New(nil, "m", t.TempDir(), "u", "h")
	mgr := convo.NewManagerWithSystem("base")
	m.SetConversation(mgr)
	eng := permissions.NewEngine(permissions.ModeDefault, nil, nil, "", tools.AllowAll)
	wirePlanMode(m, eng, mgr)

	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if !m.planOn || !m.StatusBar.PlanMode || eng.Mode() != permissions.ModePlan {
		t.Fatalf("on=%v bar=%v mode=%v", m.planOn, m.StatusBar.PlanMode, eng.Mode())
	}
	if !strings.HasSuffix(mgr.SystemPrompt(), planModePrompt) {
		t.Fatalf("prompt %q", mgr.SystemPrompt())
	}

	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.planOn || m.StatusBar.PlanMode || eng.Mode() != permissions.ModeDefault {
		t.Fatalf("left on=%v mode=%v", m.planOn, eng.Mode())
	}
	if strings.Contains(mgr.SystemPrompt(), "Plan mode is on") {
		t.Fatalf("prompt still has plan text: %q", mgr.SystemPrompt())
	}
}

func TestPlanModeRestoresBypass(t *testing.T) {
	m := New(nil, "m", t.TempDir(), "u", "h")
	eng := permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll)
	wirePlanMode(m, eng, nil)
	m.togglePlanMode()
	m.togglePlanMode()
	if eng.Mode() != permissions.ModeBypass {
		t.Fatalf("mode %v", eng.Mode())
	}
}

func TestPlanCommandStaysAReplyWhilePlanModeIsOn(t *testing.T) {
	m := New(nil, "m", t.TempDir(), "u", "h")
	m.planOn = true
	var got string
	m.runFunc = func(input string) tea.Cmd {
		got = input
		return nil
	}
	_, ok := m.handleBuiltinSlash("/plan design/ADR.md rollout")
	if !ok {
		t.Fatal("not handled")
	}
	if strings.Contains(got, "using write_file") || !strings.Contains(got, "Put the plan in your reply") {
		t.Fatalf("prompt %q", got)
	}
}
