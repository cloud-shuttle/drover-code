package permissionprompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/agent"
)

func TestPermissionPrompt_View(t *testing.T) {
	tests := []struct {
		name         string
		prompt       *PermissionPrompt
		wantContains []string
	}{
		{
			name: "basic single",
			prompt: &PermissionPrompt{
				ToolName:   "bash",
				Summary:    "Run command",
				InputJSON:  json.RawMessage(`{"command":"echo hi"}`),
				Width:      80,
			},
			wantContains: []string{"Tool permission required", "bash", "Run command", "command: echo hi", "y ", "a ", "n "},
		},
		{
			name: "narrow",
			prompt: &PermissionPrompt{
				ToolName:  "edit",
				Summary:   "edit file",
				Width:     30,
			},
			wantContains: []string{"edit", "edit file"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.prompt.View()
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("View() missing %q in output", want)
				}
			}
		})
	}
}

func TestPermissionBatchPrompt_View(t *testing.T) {
	p := &PermissionBatchPrompt{
		Items: []agent.PermissionBatchItem{
			{ToolName: "bash", Summary: "echo"},
			{ToolName: "read", Summary: "file"},
		},
		Width: 80,
	}
	got := p.View()
	if !strings.Contains(got, "Review planned tool operations") {
		t.Error("missing batch title")
	}
	if !strings.Contains(got, "bash — echo") || !strings.Contains(got, "read — file") {
		t.Error("missing batch items")
	}
	if !strings.Contains(got, "y ") || !strings.Contains(got, "a ") || !strings.Contains(got, "n ") {
		t.Error("missing batch hints")
	}
}