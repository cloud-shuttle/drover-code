package liveregion

import (
	"strings"
	"testing"

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
			region: &LiveRegion{
				Streaming:   true,
				StreamLines: "Hello there.\nThis is a streaming response.",
				Width:       80,
			},
			wantContains: []string{"Hello there", "streaming response"},
		},
		{
			name: "streaming is truncated",
			region: &LiveRegion{
				Streaming:   true,
				StreamLines: strings.Repeat("line\n", 50),
				Width:       80,
			},
			wantContains: []string{"line"},
		},
		{
			name: "narrow width",
			region: &LiveRegion{
				Streaming:   true,
				StreamLines: "This is a test of narrow terminal handling.",
				Width:       20,
			},
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
				l.StreamLines = "Analyzing the file now..."
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
