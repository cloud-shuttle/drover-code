package toolspinner

import (
	"strings"
	"testing"
)

func TestToolSpinner_View(t *testing.T) {
	tests := []struct {
		name         string
		spinner      *ToolSpinner
		wantContains []string
	}{
		{
			name: "basic",
			spinner: &ToolSpinner{
				Name:    "bash",
				Summary: "echo hello",
			},
			wantContains: []string{"bash", "echo hello"},
		},
		{
			name: "with long summary",
			spinner: &ToolSpinner{
				Name:    "read_file",
				Summary: "/very/long/path/to/some/file/that/might/be/truncated/in/the/ui",
			},
			wantContains: []string{"read_file", "/very/long/path"},
		},
		{
			name: "empty summary",
			spinner: &ToolSpinner{
				Name:    "ls",
				Summary: "",
			},
			wantContains: []string{"ls"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spinner.View()

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("View() = %q, expected to contain %q", got, want)
				}
			}
		})
	}
}
