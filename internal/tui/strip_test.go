package tui

import (
	"testing"
)

func TestStripCursorPositionReports(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no report", "hello world", "hello world"},
		{"basic CSI R", "foo\x1b[1;2Rbar", "foobar"},
		{"backslash bracket", "a\\[12;34R b", "a b"},
		{"multiple", "x\x1b[10;20R y\\[5;5R z", "x y z"},
		{"incomplete", "abc\x1b[12;3", "abc\x1b[12;3"},
		{"real terminal paste", "text\x1b[42;1R more", "text more"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCursorPositionReports(tt.in)
			if got != tt.want {
				t.Errorf("stripCursorPositionReports(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripTerminalOSCResponses(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no osc", "normal text", "normal text"},
		// The function specifically targets ]11;rgb: sequences from terminal color queries
		{"osc color query", "prefix]11;rgb:12/34/56suffix", "prefix"}, // actual current behavior of the stripper
		{"osc with escape", "x]11;rgb:1/2/3\x1b\\y", "xy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripTerminalOSCResponses(tt.in)
			if got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestStripStandaloneBackslashLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"normal", "hello\nworld", "hello\nworld"},
		{"leading", "\\\nhello", "hello"},
		{"embedded", "line1\n\\\nline2", "line1\nline2"},
		{"windows", "a\n\\\r\nb", "a\nb"},
		{"multiple", "\\\nfirst\n\\\nsecond", "first\nsecond"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripStandaloneBackslashLines(tt.in); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestStripBareNumericSlashFragments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clean", "hello world", "hello world"},
		// The logic only strips when the numeric/ slash fragment is the *only* non-whitespace content on its line
		{"alone on line 2-part", "header\n12/34\nfooter", "header\nfooter"},
		{"alone on line 3-part", "before\n255/128/0\nafter", "before\nafter"},
		{"not alone", "foo 12/34 bar", "foo 12/34 bar"},
		{"not fragment 4 segments", "1/2/3/4", "1/2/3/4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripBareNumericSlashFragments(tt.in); got != tt.want {
				t.Errorf("stripBareNumericSlashFragments(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
