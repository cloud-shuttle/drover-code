package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExpander(t *testing.T) {
	workDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(workDir, "test.txt")
	os.WriteFile(testFile, []byte("file content here"), 0644)

	expander := NewTemplateExpander(workDir)
	ctx := context.Background()

	tests := []struct {
		name     string
		template string
		args     []string
		want     string
	}{
		{
			name:     "positional args",
			template: "hello $1 $2",
			args:     []string{"world", "test"},
			want:     "hello world test",
		},
		{
			name:     "bulk args",
			template: "args: $ARGUMENTS",
			args:     []string{"a", "b", "c"},
			want:     "args: a b c",
		},
		{
			name:     "placeholders",
			template: "run {cmd|ls}",
			args:     []string{},
			want:     "run ls",
		},
		{
			name:     "file inclusion",
			template: "read @test.txt",
			args:     []string{},
			want:     "read \n\n--- File: test.txt ---\nfile content here\n--- End of test.txt ---\n\n",
		},
		{
			name:     "shell command",
			template: "run !`echo hello`",
			args:     []string{},
			want:     "run hello\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expander.Expand(ctx, tt.template, tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
