package inputarea

import (
	"strings"
	"testing"
)

func TestInputArea_SetSize(t *testing.T) {
	ia := New()
	ia.SetSize(80)
	if ia.Width != 80 {
		t.Errorf("expected width 80, got %d", ia.Width)
	}
}

func TestInputArea_View_Basic(t *testing.T) {
	ia := New()
	ia.SetSize(60)
	ia.SetValue("hello world")

	v := ia.View()
	if v == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(v, "hello world") {
		t.Errorf("expected content in view, got: %q", v)
	}
}

func TestInputArea_QueueBanner(t *testing.T) {
	ia := New()
	ia.SetSize(60)
	ia.SetMessageQueue([]string{"task 1", "task 2"})

	v := ia.View()
	if !strings.Contains(v, "2 message(s) queued") {
		t.Errorf("expected queue banner, got: %q", v)
	}
}

func TestInputArea_AutoCompleteDropdown(t *testing.T) {
	ia := New()
	ia.SetSize(60)
	ia.SetAutoState(true, 0, []Suggestion{
		{Name: "clear", Desc: "clear conversation history"},
		{Name: "compact", Desc: "summarise and compress context"},
	})

	v := ia.View()
	if !strings.Contains(v, "/clear") || !strings.Contains(v, "clear conversation") {
		t.Errorf("expected autocomplete items, got: %q", v)
	}
}

func TestInputArea_AutoCompleteSelection(t *testing.T) {
	ia := New()
	ia.SetSize(60)
	ia.SetAutoState(true, 1, []Suggestion{
		{Name: "clear", Desc: "clear conversation history"},
		{Name: "compact", Desc: "summarise and compress context"},
	})

	v := ia.View()
	// The selected row should use the selected style (we just check it renders the second item)
	if !strings.Contains(v, "/compact") {
		t.Errorf("expected second item visible, got: %q", v)
	}
}

func TestInputArea_NarrowWidth(t *testing.T) {
	ia := New()
	ia.SetSize(20) // very narrow
	ia.SetValue("x")

	v := ia.View()
	if v == "" {
		t.Error("expected view even on narrow width (no crash)")
	}
}