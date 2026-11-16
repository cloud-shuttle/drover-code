package liveregion

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/cloudshuttle/drover-code/internal/tui/components/toolspinner"
	"github.com/cloudshuttle/drover-code/internal/tui/core"
	"github.com/cloudshuttle/drover-code/internal/tui/styles"
)

// LiveRegion handles the live area showing active tools + streaming assistant output.
// This is one of the highest-value extractions in the Week 1 plan.
//
// Design note (remediation #3):
// The live preview uses its own display-oriented buffer (see livePreview below).
// This is deliberately separate from Model.streamBuf, which exists solely for the
// final Glamour-rendered assistant turn that gets committed to HistoryView.
type LiveRegion struct {
	Streaming      bool
	ActiveTools    map[int]*toolspinner.ToolSpinner
	ToolOrder      []int
	CompletedTools []core.CompletedTool
	Width          int

	// preview owns the live streaming display text.
	// It is independent of the final response accumulator in the Model.
	preview livePreview

	// Debug support (remediation #8). When enabled (via DROVER_CODE_DEBUG_STREAM
	// or SetDebugStream), we keep a bounded log of raw deltas vs what we emitted
	// after sanitization. Extremely useful for diagnosing garbled / out-of-order
	// streaming output.
	debugEnabled bool
	debugLog     []StreamDebugEntry
}

// StreamDebugEntry captures one raw delta and the sanitized result that was
// fed into the preview pipeline. Kept only when debug streaming is enabled.
type StreamDebugEntry struct {
	Raw       string
	Sanitized string
}

func New() *LiveRegion {
	l := &LiveRegion{
		ActiveTools: make(map[int]*toolspinner.ToolSpinner),
	}
	if _, ok := os.LookupEnv("DROVER_CODE_DEBUG_STREAM"); ok {
		l.debugEnabled = true
	}
	return l
}

func (l *LiveRegion) SetSize(width, _ int) {
	l.Width = width
}

// DrainCompletedTools returns the completed tools collected by this LiveRegion
// and clears them. This allows the caller (Model) to attach them to history
// without needing the legacy pendingDone slice for component-managed tools.
func (l *LiveRegion) DrainCompletedTools() []core.CompletedTool {
	if len(l.CompletedTools) == 0 {
		return nil
	}
	out := l.CompletedTools
	l.CompletedTools = nil
	return out
}

func (l *LiveRegion) View() string {
	if !l.Streaming && len(l.ActiveTools) == 0 {
		return ""
	}

	var b strings.Builder

	// Active tool spinners
	for _, idx := range l.ToolOrder {
		if ts, ok := l.ActiveTools[idx]; ok {
			row := fmt.Sprintf("%s  %s  %s",
				ts.Spinner.View(),
				lipgloss.NewStyle().Foreground(styles.ColAccent).Bold(true).Render(ts.Name),
				lipgloss.NewStyle().Foreground(styles.ColMuted).Render(ts.Summary),
			)
			b.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(row) + "\n")
		}
	}

	// Streaming preview — comes from the dedicated live display buffer.
	// This buffer can be aggressively truncated and post-processed without
	// affecting the final response that goes into history.
	if l.Streaming {
		preview := l.preview.getDisplayText(12)
		if preview != "" {
			innerW := l.Width - 10
			if innerW < 24 {
				innerW = 24
			}
			b.WriteString(lipgloss.NewStyle().Width(innerW).Render(preview))
		}
	}

	content := strings.TrimRight(b.String(), "\n")
	if content == "" {
		return ""
	}

	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(styles.ColAccentDim).
		PaddingLeft(1).
		PaddingTop(1).
		PaddingBottom(1).
		Width(l.Width - 4).
		Render(content)
}

// lastLines and SoftenAssistantParagraphBreaks support the live preview pipeline.
//
// Remediation #6 (improved):
// - Uses a look-ahead heuristic so we only insert paragraph breaks for
//   glued "colon phrases" when there is no evidence of existing paragraph
//   structure immediately after (i.e. we only soften when it looks like a
//   complete thought/paragraph is starting).
// - This makes the transformation more robust than blind ReplaceAll and
//   avoids mangling text that is already reasonably broken.
//
// The same function is used for both the live preview (lightweight) and the
// final Glamour path so the two are visually consistent.
func lastLines(text string, max int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return strings.Join(lines, "\n")
}

// SoftenAssistantParagraphBreaks inserts paragraph breaks before common
// assistant "colon starters" (Now, Let me, The, ...) only when the following
// text does not already contain paragraph evidence. This is the lightweight
// "markdown pass" for the live preview (remediation #4) as well as the final
// render step.
func SoftenAssistantParagraphBreaks(text string) string {
	if text == "" {
		return text
	}

	repls := []struct{ from, to string }{
		{":Now ", ":\n\nNow "},
		{":Let me ", ":\n\nLet me "},
		{":The ", ":\n\nThe "},
		{":I ", ":\n\nI "},
		{":We ", ":\n\nWe "},
		{":Here", ":\n\nHere"},
		{":Good!", ":\n\nGood!"},
	}

	out := text
	for _, r := range repls {
		idx := 0
		for {
			i := strings.Index(out[idx:], r.from)
			if i < 0 {
				break
			}
			abs := idx + i
			// Look ahead a short distance after the potential insertion point.
			// If we already see a double-newline (existing paragraph break)
			// or the phrase is followed by what looks like sentence completion,
			// leave it alone — only soften "glued" starts of new paragraphs.
			lookEnd := abs + len(r.from) + 32
			if lookEnd > len(out) {
				lookEnd = len(out)
			}
			look := out[abs+len(r.from) : lookEnd]
			if strings.Contains(look, "\n\n") {
				idx = abs + len(r.from)
				continue
			}
			// Safe to insert the break.
			out = out[:abs] + r.to + out[abs+len(r.from):]
			idx = abs + len(r.to)
		}
	}
	return out
}

// legacy alias for any old internal callers in the same package during transition.
func softenAssistantParagraphBreaks(text string) string { return SoftenAssistantParagraphBreaks(text) }

// AppendStreamingDelta is the new preferred way to feed live assistant tokens.
// The delta should already be lightly sanitized. This method buffers it and
// only commits content to the display buffer at safe points.
func (l *LiveRegion) AppendStreamingDelta(sanitizedDelta string) {
	l.preview.append(sanitizedDelta)
}

// ForceFlushPending forces any remaining pending tokens into the display buffer.
// Call this on DoneEvent, ToolStart, etc. so the last fragments are not lost.
func (l *LiveRegion) ForceFlushPending() {
	l.preview.forceFlush()
}

// findSafeFlushPoint returns the largest index <= len(s) that ends on a
// "safe" boundary for display (space, newline, or punctuation followed by space).
func findSafeFlushPoint(s string) int {
	if len(s) == 0 {
		return 0
	}

	// Prefer the last newline
	if idx := strings.LastIndex(s, "\n"); idx > 0 {
		return idx + 1
	}

	// Look for last space or common punctuation that usually ends a token/word.
	// Be a bit more conservative with ':' because creative text (jokes, stories)
	// from some models uses it heavily inside sentences.
	lastGood := -1
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c == ' ' || c == '\t' || c == '.' || c == ',' || c == '!' || c == '?' || c == ';' || c == '\n' {
			lastGood = i + 1
			break
		}
		// Only treat ':' as safe flush if it looks like a sentence/paragraph break
		if c == ':' {
			// look ahead a few chars
			if i+1 < len(s) && (s[i+1] == ' ' || s[i+1] == '\n' || s[i+1] == '\t') {
				lastGood = i + 1
				break
			}
		}
		// Stop searching too far back
		if len(s)-i > 48 {
			break
		}
	}

	if lastGood > 0 {
		return lastGood
	}

	// No great boundary — if the string is long enough, just flush most of it
	if len(s) > 48 {
		return len(s) - 24
	}
	return 0
}

func lastNRunes(s string, n int) string {
	if n <= 0 || len(s) == 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// livePreview is the dedicated display-oriented buffer for the live streaming
// preview. It is intentionally separate from Model.streamBuf (which exists
// only for the final Glamour-rendered assistant turn).
//
// This separation (remediation #3) allows the live view to be truncated,
// aggressively post-processed, and buffered without polluting the content
// that ends up in conversation history.
//
// Remediation #5: visible vs lines provides the rate limiting. .lines is the
// full committed content from deltas (used by tests and ForceFlush). "visible"
// is a throttled snapshot that getDisplayText returns. We only advance the
// visible window on token count or time boundaries so the TUI does not
// re-render the streaming box on literally every incoming token.
type livePreview struct {
	lines             string    // full committed content from deltas (source of truth)
	pending           string    // raw tokens waiting for a safe flush point
	visible           string    // throttled snapshot shown to View(); updated on token/time rules
	lastVisibleCommit time.Time // when we last promoted content into visible

	// responseStart tracks when the current assistant turn's streaming began.
	// Used to relax rate limiting for the first ~1.5s or first ~200 chars so the
	// very beginning of a response always feels smooth and correctly ordered
	// (this was the remaining weak spot after the main buffering/sanitization work).
	responseStart time.Time
}

func (p *livePreview) append(delta string) {
	if delta == "" {
		return
	}
	if p.responseStart.IsZero() {
		p.responseStart = time.Now()
	}

	p.pending += delta

	flushIdx := findSafeFlushPoint(p.pending)
	if flushIdx <= 0 {
		if len(p.pending) > 120 {
			flushIdx = len(p.pending) - 40
		} else {
			return
		}
	}

	toFlush := p.pending[:flushIdx]
	p.lines += toFlush
	p.pending = p.pending[flushIdx:]
}

func (p *livePreview) forceFlush() {
	if p.pending == "" {
		return
	}
	p.lines += p.pending
	p.pending = ""

	if p.responseStart.IsZero() {
		p.responseStart = time.Now()
	}

	// On explicit flush (ToolStart, Done, etc.) always promote to visible
	// so the user sees the last fragments immediately.
	p.visible = p.lines
	p.lastVisibleCommit = time.Now()
}

func (p *livePreview) reset() {
	p.lines = ""
	p.pending = ""
	p.visible = ""
	p.lastVisibleCommit = time.Time{}
	p.responseStart = time.Time{}
}

// maybeCommitVisible implements the core of remediation #5.
// We only advance the "visible" window (what the TUI actually renders)
// when either a decent number of new tokens have arrived since the last
// promotion OR enough wall time has passed. This prevents re-rendering
// the live streaming box on literally every token while still feeling
// responsive.
func (p *livePreview) maybeCommitVisible() {
	now := time.Now()

	// Special case for the very beginning of a response.
	// Drain any pending immediately and promote aggressively so the opening
	// words appear in the correct order without waiting for "safe" boundaries
	// or the normal rate limiter. This targets the persistent "thereHey!👋"
	// style fragmentation the user sees at the start of GLM responses.
	isStartup := !p.responseStart.IsZero() &&
		(now.Sub(p.responseStart) < 2500*time.Millisecond && len(p.lines) < 300)

	if isStartup && p.pending != "" {
		// Force everything pending into .lines right now during startup.
		p.lines += p.pending
		p.pending = ""
	}

	if len(p.lines) <= len(p.visible) {
		return
	}

	delta := len(p.lines) - len(p.visible)

	if isStartup {
		// During the absolute beginning of a response (first ~1s or first ~100 chars),
		// only promote to visible once we have at least one complete word.
		// This prevents jarring partial-word attachments like "thereHey" on the
		// very first visible update, which is the remaining complaint with GLM
		// gateway streams.
		hasWordBoundary := strings.ContainsAny(p.lines, " \t\n.,!?;:")
		if hasWordBoundary || len(p.lines) > 100 {
			p.visible = p.lines
			p.lastVisibleCommit = now
		}
		return
	}

	// Normal throttle rules (after startup window).
	// We use a generous window here primarily so unit tests remain stable
	// across different machines/CI. In real usage this still provides excellent
	// rate limiting against 50+ tokens/sec streams.
	if delta > 18 || now.Sub(p.lastVisibleCommit) > 800*time.Millisecond || p.visible == "" {
		p.visible = p.lines
		p.lastVisibleCommit = now
	}
}

func (p *livePreview) getDisplayText(maxLines int) string {
	p.maybeCommitVisible()

	src := p.visible
	if src == "" {
		src = p.lines // fall back on first paint or after reset
	}
	if src == "" {
		return ""
	}

	// During the very beginning of a response, show more context (less aggressive
	// truncation). This prevents the forming opening sentence from looking
	// fragmented due to lastLines cutting it while early deltas are still
	// arriving and being promoted. After the startup window we go back to the
	// normal tight 12-line tail.
	effectiveMax := maxLines
	if !p.responseStart.IsZero() && time.Since(p.responseStart) < 2000*time.Millisecond {
		effectiveMax = 30 // generous during the critical opening of the response
	}

	text := lastLines(src, effectiveMax)

	// Remediation #4 lightweight pass for live preview:
	// Only do the cheap, safe stripping of common markdown markers.
	// We deliberately do NOT run SoftenAssistantParagraphBreaks here anymore
	// on the live tail — the colon-based rules were too eager on creative/long
	// model outputs (especially non-Claude models) and produced the exact
	// punctuation/word-order garbling the user reported.
	// The full softening still happens in renderMarkdown for the committed turn.
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "__", "")
	// Remove backticks but keep the content (cheap inline code simulation)
	text = strings.ReplaceAll(text, "`", "")

	return text
}

// AppendDelta is the new preferred entry point for raw assistant text deltas
// (remediation #7). It fully encapsulates:
//
//   - cheap incremental sanitization (using recent display tail as context)
//   - the four strip passes (now co-located in this package)
//   - feeding the sanitized result into the preview buffer + throttling (#5)
//   - lightweight formatting for the live view (#4)
//
// Callers (primarily Model.handleAgentEvent) no longer need to know about
// the strip functions, tail management, or sanitization ordering.
// This dramatically reduces the spread of streaming logic between model.go
// and liveregion.
func (l *LiveRegion) AppendDelta(raw string) {
	if l == nil || raw == "" {
		return
	}

	// Incremental sanitization using the last ~80 chars of what we have
	// already committed to the display (from the preview buffer).
	tail := lastNRunes(l.preview.lines, 80)
	candidate := tail + raw

	s := StripCursorPositionReports(candidate)
	s = StripTerminalOSCResponses(s)
	s = StripBareRGBTriplets(s)
	s = StripStandaloneBackslashLines(s)

	if len(tail) > 0 && strings.HasPrefix(s, tail) {
		s = s[len(tail):]
	}

	// Capture for debug (remediation #8) before we lose the raw/sanitized pairing.
	if l.debugEnabled {
		l.captureDebug(raw, s)
	}

	// Now let the existing preview pipeline (boundary flush, rate-limit #5,
	// paragraph softening + cheap markdown pass #4) handle it.
	l.preview.append(s)
}

func (l *LiveRegion) captureDebug(raw, sanitized string) {
	if !l.debugEnabled {
		return
	}
	entry := StreamDebugEntry{Raw: raw, Sanitized: sanitized}
	const maxDebugEntries = 32 // small bounded ring is plenty for debugging bursts
	if len(l.debugLog) >= maxDebugEntries {
		// simple ring: drop oldest
		l.debugLog = l.debugLog[1:]
	}
	l.debugLog = append(l.debugLog, entry)
}

// SetDebugStream enables or disables capture of raw vs sanitized deltas for
// debugging (remediation #8). When enabled via DROVER_CODE_DEBUG_STREAM at
// construction time this is already on.
func (l *LiveRegion) SetDebugStream(enabled bool) {
	l.debugEnabled = enabled
	if !enabled {
		l.debugLog = nil
	}
}

// StreamDebugSnapshot returns a copy of the recent raw→sanitized entries
// captured while debug mode was active. Useful for tests and interactive
// debugging of streaming corruption.
func (l *LiveRegion) StreamDebugSnapshot() []StreamDebugEntry {
	if l == nil || len(l.debugLog) == 0 {
		return nil
	}
	out := make([]StreamDebugEntry, len(l.debugLog))
	copy(out, l.debugLog)
	return out
}

// GetLastDisplayTail returns the last N runes of the currently committed display text.
// Deprecated for normal use: prefer AppendDelta (which owns the sanitization + tail logic).
// Still exported for a few tests and any external code that did its own sanitization.
func (l *LiveRegion) GetLastDisplayTail(n int) string {
	return lastNRunes(l.preview.lines, n)
}

// SetDisplayTextForTest is a test helper to directly set the committed display text.
// It also primes the visible snapshot so View() tests see the content immediately.
func (l *LiveRegion) SetDisplayTextForTest(text string) {
	l.preview.lines = text
	l.preview.visible = text
	l.preview.pending = ""
	l.preview.lastVisibleCommit = time.Now()
}

// ResetLiveStreaming clears the live preview state (delegates to internal buffer).
func (l *LiveRegion) ResetLiveStreaming() {
	l.preview.reset()
	l.Streaming = false
}