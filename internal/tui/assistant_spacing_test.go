package tui

import (
	"strings"
	"testing"
)

func TestSoftenAssistantParagraphBreaks(t *testing.T) {
	in := "check the Foo struct:Now let me read bar.go:Let me see:The end"
	out := softenAssistantParagraphBreaks(in)
	if out == in {
		t.Fatal("expected insertions")
	}
	if !containsInOrder(out, "struct:", "\n\nNow let me", ".go:", "\n\nLet me") {
		t.Fatalf("got %q", out)
	}
}

func containsInOrder(s string, parts ...string) bool {
	pos := 0
	for _, p := range parts {
		i := strings.Index(s[pos:], p)
		if i < 0 {
			return false
		}
		pos += i + len(p)
	}
	return true
}
