package liveregion

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tui/components/toolspinner"
)

func TestLiveRegion_View(t *testing.T) {
	tests := []struct {
		name         string
		region       *LiveRegion
		wantContains []string
		wantEmpty    bool
	}{
		{
			name: "empty",
			region: &LiveRegion{
				Width: 80,
			},
			wantEmpty: true,
		},
		{
			name: "with one active tool",
			region: func() *LiveRegion {
				l := New()
				l.Width = 80
				l.ActiveTools[0] = toolspinner.New("bash", "echo hello")
				l.ToolOrder = []int{0}
				return l
			}(),
			wantContains: []string{"bash", "echo hello"},
		},
		{
			name: "with streaming text",
			region: func() *LiveRegion {
				l := New()
				l.Streaming = true
				l.SetDisplayTextForTest("Hello there.\nThis is a streaming response.")
				l.Width = 80
				return l
			}(),
			wantContains: []string{"Hello there", "streaming response"},
		},
		{
			name: "streaming is truncated",
			region: func() *LiveRegion {
				l := New()
				l.Streaming = true
				l.SetDisplayTextForTest(strings.Repeat("line\n", 50))
				l.Width = 80
				return l
			}(),
			wantContains: []string{"line"},
		},
		{
			name: "narrow width",
			region: func() *LiveRegion {
				l := New()
				l.Streaming = true
				l.SetDisplayTextForTest("This is a test of narrow terminal handling.")
				l.Width = 20
				return l
			}(),
			wantContains: []string{"This is a test"},
		},
		{
			name: "tools + streaming together",
			region: func() *LiveRegion {
				l := New()
				l.Width = 80
				l.ActiveTools[0] = toolspinner.New("read_file", "foo.txt")
				l.ToolOrder = []int{0}
				l.Streaming = true
				l.SetDisplayTextForTest("Analyzing the file now...")
				return l
			}(),
			wantContains: []string{"read_file", "Analyzing the file"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.region.View()

			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty View(), got %q", got)
				}
				return
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("View() = %q, expected to contain %q", got, want)
				}
			}
		})
	}
}

func TestLiveRegion_SetSize(t *testing.T) {
	l := New()
	l.SetSize(120, 0)
	if l.Width != 120 {
		t.Errorf("expected Width 120, got %d", l.Width)
	}
}

func TestLiveRegion_AppendStreamingDelta_PreservesWordOrder(t *testing.T) {
	lr := New()
	lr.SetSize(80, 24)

	// Simulate rapid token streaming with many word splits, just like real LLM output.
	chunks := []string{
		"Hel", "lo", " ", "wor", "ld", "! ", "This", " ", "is", " ", "a", " ",
		"test", " ", "of", " ", "the", " ", "new", " ", "stream", "ing", " ",
		"buf", "fer", ".", "\n\n", "It", " ", "should", " ", "not", " ", "corrupt",
		" ", "word", " ", "order", " ", "even", " ", "under", " ", "heavy", " ",
		"token", " ", "splitting", ".",
	}

	for _, chunk := range chunks {
		lr.AppendStreamingDelta(chunk)
	}

	// Force final flush (simulates end of generation)
	lr.ForceFlushPending()

	expected := "Hello world! This is a test of the new streaming buffer.\n\nIt should not corrupt word order even under heavy token splitting."
	if !strings.Contains(lr.preview.lines, expected) {
		t.Errorf("word order corrupted in preview\n got: %q\nwant to contain: %q", lr.preview.lines, expected)
	}

	// Verify via View() as well — we must set Streaming so View() doesn't early-return empty
	lr.Streaming = true
	view := lr.View()
	if !strings.Contains(view, "Hello world") || !strings.Contains(view, "streaming buffer") {
		t.Errorf("View() output lost word order: %q", view)
	}
}

func TestLiveRegion_AppendStreamingDelta_FlushesOnBoundaries(t *testing.T) {
	lr := New()
	lr.SetSize(80, 24)

	lr.AppendStreamingDelta("Hello")
	// No flush should have happened yet (no boundary)
	if lr.preview.lines != "" {
		t.Errorf("expected no flush yet, got preview=%q", lr.preview.lines)
	}

	lr.AppendStreamingDelta(" world!")
	// Now we should have flushed because of the space and punctuation
	if !strings.Contains(lr.preview.lines, "Hello world") {
		t.Errorf("expected flush after boundary, got preview=%q", lr.preview.lines)
	}
}

// TestLiveRegion_Throttle_RateLimitsVisibleUpdates verifies remediation #5.
// Rapid tiny deltas should not cause the *visible* snapshot (what View returns)
// to advance on every single append. We only promote on token-count or time
// boundaries. .lines (the source buffer) still receives everything.
func TestLiveRegion_Throttle_RateLimitsVisibleUpdates(t *testing.T) {
	lr := New()
	lr.SetSize(80, 24)
	lr.Streaming = true

	// Seed with something that flushes (has boundary) so we have a visible baseline.
	lr.AppendStreamingDelta("Hello world. ")
	_ = lr.View()
	if lr.preview.visible == "" {
		t.Fatal("expected initial flush to promote to visible")
	}

	startVisibleLen := len(lr.preview.visible)

	// Force the last promotion time far in the future so the time-based throttle
	// can never fire during this tiny burst, making the test fully deterministic
	// regardless of how long the test takes to execute on any machine.
	lr.preview.lastVisibleCommit = time.Now().Add(10 * time.Hour)

	// Move the response start outside the startup window (2.5s / 300 chars).
	// During startup, maybeCommitVisible intentionally drains pending into
	// .lines and promotes it (see TestLiveRegion_Startup_DrainsPendingImmediately);
	// this test covers the steady-state throttle that applies afterwards.
	lr.preview.responseStart = time.Now().Add(-10 * time.Second)

	// Now do a burst of tiny non-boundary appends whose total growth is well
	// under the 18-char promotion threshold, all within the same millisecond.
	// These stay in pending until we hit a boundary or ForceFlush.
	for i := 0; i < 4; i++ {
		lr.AppendStreamingDelta("xyz") // 3 chars each, no spaces/punct → stay pending
	}

	_ = lr.View()

	// The visible window must not have grown at all from the pending-only burst
	// (throttle + no flush to .lines yet).
	if len(lr.preview.visible) != startVisibleLen {
		t.Errorf("throttle or pending logic allowed visible growth on tiny non-flushing burst: before=%d after=%d (lines=%d)",
			startVisibleLen, len(lr.preview.visible), len(lr.preview.lines))
	}

	// .lines still only has the initial flushed part (pending not yet in .lines)
	if !strings.Contains(lr.preview.lines, "Hello world") {
		t.Errorf("initial content missing from .lines: %q", lr.preview.lines)
	}

	// ForceFlush promotes pending into .lines *and* forces visible
	lr.ForceFlushPending()
	viewAfter := lr.View()
	if !strings.Contains(viewAfter, "Hello world") || !strings.Contains(lr.preview.lines, "xyzxyzxyzxyz") {
		t.Errorf("after ForceFlush, expected full text in lines and view. lines=%q view=%q",
			lr.preview.lines, viewAfter)
	}
}

func TestLiveRegion_Startup_DrainsPendingImmediately(t *testing.T) {
	lr := New()
	lr.SetSize(80, 24)
	lr.Streaming = true

	lr.AppendStreamingDelta("Hello world. ")
	_ = lr.View()
	startVisibleLen := len(lr.preview.visible)

	// Within the startup window, tiny non-boundary fragments are drained into
	// .lines and promoted on the next View so opening words never fragment.
	for i := 0; i < 4; i++ {
		lr.AppendStreamingDelta("xyz")
	}
	_ = lr.View()

	if lr.preview.pending != "" {
		t.Errorf("expected startup drain to empty pending, got %q", lr.preview.pending)
	}
	if !strings.Contains(lr.preview.visible, "xyzxyzxyzxyz") || len(lr.preview.visible) <= startVisibleLen {
		t.Errorf("expected startup promotion of drained fragments: before=%d visible=%q",
			startVisibleLen, lr.preview.visible)
	}
}

func TestLiveRegion_DebugStream_RawVsSanitized(t *testing.T) {
	lr := New()
	lr.SetSize(80, 24)
	lr.SetDebugStream(true) // explicit, even without the env var

	// Use the new high-level entry point (remediation #7)
	lr.AppendDelta("Hello\x1b[1;2R world!") // contains a CSI report that should be stripped

	snap := lr.StreamDebugSnapshot()
	if len(snap) == 0 {
		t.Fatal("expected at least one debug entry after AppendDelta with debug enabled")
	}
	last := snap[len(snap)-1]
	if last.Raw == "" || last.Sanitized == "" {
		t.Errorf("debug entry missing raw or sanitized: %+v", last)
	}
	if strings.Contains(last.Sanitized, "\x1b[") {
		t.Errorf("sanitized output still contained CSI report: %q", last.Sanitized)
	}
	if !strings.Contains(last.Sanitized, "Hello") || !strings.Contains(last.Sanitized, "world") {
		t.Errorf("sanitized lost content: raw=%q sanitized=%q", last.Raw, last.Sanitized)
	}
}
