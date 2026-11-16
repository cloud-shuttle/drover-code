package liveregion

import "strings"

// The strip* functions perform defensive cleanup of terminal control sequences,
// OSC responses, bare RGB fragments, and other junk that sometimes leaks into
// LLM token streams (especially over certain transports or when the model
// hallucinates terminal output).
//
// Remediation #7 context: These now live inside the liveregion package so that
// the full streaming pipeline (sanitization + buffering + preview) can be
// encapsulated without the parent model having to know the details.

func StripCursorPositionReports(s string) string {
	for {
		changed := false
		if idx := strings.Index(s, "\x1b["); idx >= 0 {
			if end := scanCSIReportEnd(s, idx+2); end > idx {
				s = s[:idx] + s[end:]
				changed = true
			}
		}
		if idx := strings.Index(s, "\\["); idx >= 0 {
			if end := scanBackslashBracketReportEnd(s, idx+2); end > idx {
				s = s[:idx] + s[end:]
				changed = true
			}
		}
		if !changed {
			return s
		}
	}
}

// StripInputCursorReports cleans a whole input buffer. Cursor replies often
// arrive one byte at a time, so pass the accumulated text, not the latest
// rune. stored has complete reports removed. shown also drops a trailing
// reply that is still arriving, so it does not hide every row while the
// terminal finishes the sequence.
func StripInputCursorReports(s string) (stored, shown string) {
	stored = stripCompleteCursorReports(s)
	shown = stripTrailingCursorPosition(stored)
	return stored, shown
}

func stripCompleteCursorReports(s string) string {
	s = StripCursorPositionReports(s)
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '[' {
			if end := scanCSIReportEnd(s, i+1); end > i {
				i = end
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func stripTrailingCursorPosition(s string) string {
	if s == "" {
		return s
	}
	i := len(s) - 1
	if s[i] == '\\' || s[i] == 0x1b {
		return s[:i]
	}
	for i >= 0 && s[i] >= '0' && s[i] <= '9' {
		i--
	}
	if i >= 0 && s[i] == ';' {
		i--
		for i >= 0 && s[i] >= '0' && s[i] <= '9' {
			i--
		}
	}
	if i < 0 || s[i] != '[' {
		return s
	}
	bracket := i
	escaped := bracket > 0 && (s[bracket-1] == '\\' || s[bracket-1] == 0x1b)
	if !escaped && !strings.Contains(s[bracket:], ";") {
		return s
	}
	if escaped {
		return s[:bracket-1]
	}
	return s[:bracket]
}

func scanCSIReportEnd(s string, start int) int {
	i := start
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i >= len(s) || s[i] != ';' {
		return -1
	}
	i++
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < len(s) && s[i] == 'R' {
		return i + 1
	}
	return -1
}

func scanBackslashBracketReportEnd(s string, start int) int {
	i := start
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i >= len(s) || s[i] != ';' {
		return -1
	}
	i++
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < len(s) && s[i] == 'R' {
		return i + 1
	}
	return -1
}

func StripTerminalOSCResponses(s string) string {
	for {
		// Detect common variants of terminal color query responses (OSC 11).
		// Terminals (and some TUI color probing) reply with things like:
		//   ]11;rgb:RRRR/GGGG/BBBB
		//   ]11rgb:RRRR/GGGG/BBBB
		// These can arrive split across reads and leak into the focused textarea
		// on startup, producing garbage like the "]11rgb:2828/..." the user saw.
		i := strings.Index(s, "]11;rgb:")
		if i < 0 {
			i = strings.Index(s, "]11rgb:")
		}
		if i < 0 {
			i = strings.Index(s, "11;rgb:")
		}
		if i < 0 {
			i = strings.Index(s, "11rgb:")
		}
		if i < 0 {
			i = strings.Index(s, "11;rg")
		}
		if i < 0 {
			i = strings.Index(s, "11rg")
		}

		if i < 0 {
			return s
		}

		// If we landed on a fragment, try to include a preceding ']' if present.
		start := i
		if s[start] == '1' && start > 0 && s[start-1] == ']' {
			start--
		}

		// Consume until a plausible OSC terminator or whitespace.
		j := start + 1
		for j < len(s) {
			switch s[j] {
			case '\a', 0x1b, '\\', '\n', '\r', ' ':
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
				} else {
					j++
				}
				goto cut
			default:
				j++
			}
		}
	cut:
		if j > len(s) {
			j = len(s)
		}
		s = s[:start] + s[j:]
	}
}

func StripStandaloneBackslashLines(s string) string {
	for {
		orig := s
		s = strings.TrimPrefix(s, "\\\n")
		s = strings.ReplaceAll(s, "\n\\\n", "\n")
		s = strings.ReplaceAll(s, "\n\\\r\n", "\n")
		if s == orig {
			return s
		}
	}
}

func StripBareRGBTriplets(s string) string {
	return stripBareNumericSlashFragments(s)
}

func stripBareNumericSlashFragments(s string) string {
	for {
		i := indexNumericSlashFragment(s)
		if i < 0 {
			return s
		}
		start, end := numericSlashLineBounds(s, i)
		if start < 0 || end <= start {
			return s
		}
		s = s[:start] + s[end:]
	}
}

func indexNumericSlashFragment(s string) int {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' && (c < '0' || c > '9') {
			continue
		}
		_, end := scanNumericSlashFragment(s, i)
		if end <= i {
			continue
		}
		if end < len(s) && s[end] != '\n' && s[end] != '\r' && s[end] != ' ' && s[end] != '\t' {
			continue
		}
		return i
	}
	return -1
}

func scanNumericSlashFragment(s string, start int) (ok bool, end int) {
	i := start
	for i < len(s) && (s[i] == '\\' || s[i] == '/') {
		i++
	}
	segCount := 0
	for {
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return false, -1
		}
		segCount++
		i = j
		if i < len(s) && s[i] == '/' {
			i++
			continue
		}
		break
	}
	if segCount < 2 || segCount > 3 {
		return false, -1
	}
	for i < len(s) && (s[i] == '\\' || s[i] == '/') {
		i++
	}
	return true, i
}

func numericSlashLineBounds(s string, idx int) (start, end int) {
	lineStart := strings.LastIndexByte(s[:idx], '\n')
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}
	lineEnd := strings.IndexByte(s[idx:], '\n')
	if lineEnd < 0 {
		lineEnd = len(s)
	} else {
		lineEnd = idx + lineEnd + 1
	}

	line := s[lineStart:lineEnd]
	trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	if trimmed == "" {
		return -1, -1
	}

	ok, fragEnd := scanNumericSlashFragment(trimmed, 0)
	if !ok {
		return -1, -1
	}
	if strings.TrimSpace(trimmed[fragEnd:]) != "" {
		return -1, -1
	}
	return lineStart, lineEnd
}
