package historyview

import (
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/tui/core"
)

func TestHistoryView_SetSize(t *testing.T) {
	hv := New()
	hv.SetSize(80, 20)
	if hv.Width != 80 || hv.Height != 20 {
		t.Errorf("expected 80x20, got %dx%d", hv.Width, hv.Height)
	}
}

func TestHistoryView_View_Empty(t *testing.T) {
	hv := New()
	hv.SetSize(80, 10)
	// View may be empty or contain just the viewport chrome; non-crash is the requirement.
	_ = hv.View()
}

func TestHistoryView_SetTurnsAndView(t *testing.T) {
	hv := New()
	hv.SetSize(80, 20)
	hv.SetTurns([]core.RenderedTurn{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	})

	v := hv.View()
	if v == "" {
		t.Error("expected non-empty view for turns")
	}
	if !strings.Contains(v, "you") || !strings.Contains(v, "drover-code") {
		t.Errorf("expected labels in output, got: %q", v)
	}
}

func TestHistoryView_SystemRoleAndTools(t *testing.T) {
	hv := New()
	hv.SetSize(80, 20)
	hv.SetTurns([]core.RenderedTurn{
		{Role: "system", Content: "(/pause) Agent interrupted."},
		{Role: "user", Content: "next"},
		{
			Role:    "assistant",
			Content: "done",
			Tools: []core.CompletedTool{
				{Name: "bash", Summary: "ls -l", IsError: false},
				{Name: "edit_file", Summary: "foo.go:42", IsError: true},
			},
		},
	})

	v := hv.View()
	if !strings.Contains(v, "pause") {
		t.Errorf("expected system note rendered, got: %q", v)
	}
	if !strings.Contains(v, "bash") || !strings.Contains(v, "edit_file") {
		t.Errorf("expected tool rows, got: %q", v)
	}
	if !strings.Contains(v, "\u2717") { // error icon
		t.Errorf("expected error icon for failed tool, got: %q", v)
	}
}

func TestHistoryView_Truncation(t *testing.T) {
	hv := New()
	hv.SetSize(80, 20)
	hv.MaxHistoryDisplay = 2

	hv.SetTurns([]core.RenderedTurn{
		{Role: "user", Content: "one"},
		{Role: "user", Content: "two"},
		{Role: "user", Content: "three"},
		{Role: "user", Content: "four"},
	})

	v := hv.View()
	if !strings.Contains(v, "older turns hidden") {
		t.Errorf("expected truncation note, got: %q", v)
	}
	if strings.Contains(v, "one") {
		t.Errorf("expected oldest turn dropped, got: %q", v)
	}
	if !strings.Contains(v, "three") || !strings.Contains(v, "four") {
		t.Errorf("expected last 2 turns present, got: %q", v)
	}
}

func TestHistoryView_NarrowWidth(t *testing.T) {
	hv := New()
	hv.SetSize(30, 10) // very narrow
	hv.SetTurns([]core.RenderedTurn{
		{Role: "user", Content: "short"},
	})

	v := hv.View()
	// Should not panic and should contain the content (width clamping happens inside bubble).
	if !strings.Contains(v, "short") && !strings.Contains(v, "you") {
		t.Errorf("expected content even on narrow width, got: %q", v)
	}
}
