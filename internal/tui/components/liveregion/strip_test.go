package liveregion

import "testing"

// These tests cover the strip helpers that are now co-located in the liveregion
// package as part of remediation #7 (encapsulating the streaming sanitization
// pipeline together with buffering + preview logic).

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
			got := StripCursorPositionReports(tt.in)
			if got != tt.want {
				t.Errorf("StripCursorPositionReports(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripInputCursorReports(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		stored, shown string
	}{
		{"literal reply", `\[1;1R`, "", ""},
		{"csi reply", "\x1b[1;1R", "", ""},
		{"bare reply", "[1;1R", "", ""},
		{"keeps typed text", `fix\[1;1R`, "fix", "fix"},
		{"hides a reply still arriving", `\[1;1`, `\[1;1`, ""},
		{"bare digits stay", "[1", "[1", "[1"},
		{"typed backslash stays once a letter follows", `foo\x`, `foo\x`, `foo\x`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, shown := StripInputCursorReports(tt.in)
			if stored != tt.stored || shown != tt.shown {
				t.Errorf("StripInputCursorReports(%q) = stored %q shown %q, want stored %q shown %q", tt.in, stored, shown, tt.stored, tt.shown)
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
		{"osc color query", "prefix]11;rgb:12/34/56suffix", "prefix"},
		{"osc with escape", "x]11;rgb:1/2/3\x1b\\y", "xy"},
		// Variants without the semicolon after 11 (seen in the wild on macOS terminals)
		{"osc no semicolon", "garbage]11rgb:2828/2c2c/3434\\more", "garbagemore"},
		{"osc no semicolon fragment", "]11rgb:aaaa/bbbb/cccc", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripTerminalOSCResponses(tt.in)
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
			if got := StripStandaloneBackslashLines(tt.in); got != tt.want {
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
			if got := StripBareRGBTriplets(tt.in); got != tt.want {
				t.Errorf("StripBareRGBTriplets(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
