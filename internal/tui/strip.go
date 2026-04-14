package tui

import "strings"

func stripCursorPositionReports(s string) string {
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

func stripTerminalOSCResponses(s string) string {
	for {
		i := strings.Index(s, "]11;rgb:")
		if i < 0 {
			return s
		}
		j := i + len("]11;rgb:")
		for j < len(s) {
			switch s[j] {
			case '\a', 0x1b, '\\', '\n', '\r':
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
		s = s[:i] + s[j:]
	}
}

func stripStandaloneBackslashLines(s string) string {
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

func stripBareRGBTriplets(s string) string {
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
