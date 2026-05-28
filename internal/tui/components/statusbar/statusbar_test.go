package statusbar

import (
	"strings"
	"testing"
)

func TestStatusBar_View(t *testing.T) {
	tests := []struct {
		name         string
		bar          StatusBar
		wantContains []string
	}{
		{
			name: "basic idle",
			bar: StatusBar{
				ModelName:    "claude-3-5-sonnet",
				InputTokens:  1234,
				OutputTokens: 567,
				Width:        80,
			},
			wantContains: []string{"claude-3-5-sonnet", "in:1234", "out:567"},
		},
		{
			name: "busy state",
			bar: StatusBar{
				ModelName:    "claude-3-5-sonnet",
				AgentBusy:    true,
				InputTokens:  5000,
				OutputTokens: 1200,
				Width:        80,
			},
			wantContains: []string{"● LIVE", "in:5000", "out:1200"},
		},
		{
			name: "narrow terminal",
			bar: StatusBar{
				ModelName:    "sonnet",
				InputTokens:  42,
				OutputTokens: 7,
				Width:        30,
			},
			wantContains: []string{"sonnet", "in:42", "out:7"},
		},
		{
			name: "zero width",
			bar: StatusBar{
				ModelName: "anything",
				Width:     0,
			},
			wantContains: []string{""}, // should be empty
		},
		{
			name: "risk normal (default, no indicator)",
			bar: StatusBar{
				ModelName:    "sonnet",
				RiskLevel:    "normal",
				InputTokens:  100,
				OutputTokens: 50,
				Width:        80,
			},
			wantContains: []string{"sonnet", "in:100", "out:50"},
		},
		{
			name: "risk caution",
			bar: StatusBar{
				ModelName:    "sonnet",
				RiskLevel:    "caution",
				InputTokens:  100,
				OutputTokens: 50,
				Width:        80,
			},
			wantContains: []string{"sonnet", "CAUTION", "Guard:"},
		},
		{
			name: "risk high",
			bar: StatusBar{
				ModelName:    "sonnet",
				RiskLevel:    "high",
				InputTokens:  100,
				OutputTokens: 50,
				Width:        80,
			},
			wantContains: []string{"sonnet", "HIGH", "Guard:"},
		},
		{
			name: "risk with reason",
			bar: StatusBar{
				ModelName:    "sonnet",
				RiskLevel:    "caution",
				RiskReason:   "modifying source files",
				InputTokens:  100,
				OutputTokens: 50,
				Width:        80,
			},
			wantContains: []string{"CAUTION", "modifying source files"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.bar.View()

			for _, want := range tt.wantContains {
				if want == "" {
					if got != "" {
						t.Errorf("expected empty output, got %q", got)
					}
					continue
				}
				if !strings.Contains(got, want) {
					t.Errorf("View() = %q, expected to contain %q", got, want)
				}
			}
		})
	}
}