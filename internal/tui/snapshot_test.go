package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/tui/core"
)

// assertSnapshot compares the actual output with the stored golden file.
// Run tests with UPDATE_SNAPSHOTS=1 to update the golden files.
func assertSnapshot(t *testing.T, name string, actual string) {
	t.Helper()
	filename := filepath.Join("testdata", name+".golden")

	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatalf("failed to create testdata dir: %v", err)
		}
		if err := os.WriteFile(filename, []byte(actual), 0644); err != nil {
			t.Fatalf("failed to update snapshot: %v", err)
		}
		return
	}

	expected, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read snapshot (run with UPDATE_SNAPSHOTS=1 to create): %v", err)
	}

	expectedStr := strings.ReplaceAll(string(expected), "\r\n", "\n")
	actualStr := strings.ReplaceAll(actual, "\r\n", "\n")

	if expectedStr != actualStr {
		t.Errorf("snapshot mismatch for %s\nExpected:\n%s\nGot:\n%s", name, expectedStr, actualStr)
	}
}

func TestModel_SnapshotMarkdownRendering(t *testing.T) {
	// Force a predictable terminal environment for Glamour/Lipgloss
	os.Setenv("CLICOLOR_FORCE", "0")
	os.Setenv("TERM", "dumb")

	ch := make(chan agent.Event, 1)
	m := New(ch, "test-model", "/tmp/wd", "user", "host")
	m.width = 100
	m.height = 40

	// Trigger markdown compilation via HistoryView (source of truth after consolidation)
	m.HistoryView.AppendTurn(core.RenderedTurn{
		Role: "assistant",
		Content: "# Header\n\nThis is a **bold** and *italic* test.\n\n" +
			"- List item 1\n- List item 2\n\n" +
			"```go\nfunc main() {}\n```",
	})

	actual := m.HistoryView.View()
	assertSnapshot(t, "markdown_rendering", actual)
}

func TestModel_SnapshotHomeView(t *testing.T) {
	os.Setenv("CLICOLOR_FORCE", "0")
	os.Setenv("TERM", "dumb")

	ch := make(chan agent.Event, 1)
	m := New(ch, "test-model", "/tmp/wd", "user", "host")
	m.width = 100
	m.height = 40

	actual := m.viewHome()
	assertSnapshot(t, "home_view", actual)
}
