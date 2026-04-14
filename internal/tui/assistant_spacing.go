package tui

import (
	"strings"
)

// softenAssistantParagraphBreaks inserts newlines before common model phrases when
// they are glued to a colon (no space after ':'). This avoids wall-of-text in the
// live stream and improves Glamour input for completed turns.
func softenAssistantParagraphBreaks(s string) string {
	if s == "" {
		return s
	}
	// High-signal patterns from Claude-style tool narration (colon + immediate "Now"/"Let").
	repls := []struct{ from, to string }{
		{":Now ", ":\n\nNow "},
		{":Let me ", ":\n\nLet me "},
		{":The ", ":\n\nThe "},
		{":I ", ":\n\nI "},
		{":We ", ":\n\nWe "},
		{":Here", ":\n\nHere"},
		{":Good!", ":\n\nGood!"},
	}
	out := s
	for _, r := range repls {
		out = strings.ReplaceAll(out, r.from, r.to)
	}
	return out
}
