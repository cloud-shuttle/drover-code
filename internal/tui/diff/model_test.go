package diff

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var sampleUnifiedDiff = `--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 func main() {
-	fmt.Println("old")
+	fmt.Println("new")
 }
@@ -10,3 +10,3 @@
 func old() {
-	return false
+	return true
 }
`

func TestDiffModel_NavigationAndKeys(t *testing.T) {
	m := NewDiffModel("file.go", sampleUnifiedDiff)
	if len(m.GetHunks()) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(m.GetHunks()))
	}
	if m.diff.Cursor != 0 {
		t.Fatalf("cursor should start at 0, got %d", m.diff.Cursor)
	}

	// Test j / down
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = newModel.(Model)
	if m.diff.Cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", m.diff.Cursor)
	}

	// Test k / up
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = newModel.(Model)
	if m.diff.Cursor != 0 {
		t.Fatalf("expected cursor 0, got %d", m.diff.Cursor)
	}

	// Test Toggle Space
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = newModel.(Model)
	if !m.GetHunks()[0].Accepted {
		t.Fatal("expected hunk 0 to be accepted")
	}

	// Test Accept All 'A'
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = newModel.(Model)
	for i, h := range m.GetHunks() {
		if !h.Accepted {
			t.Fatalf("expected hunk %d to be accepted", i)
		}
	}

	// Test Reject All 'R'
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = newModel.(Model)
	for i, h := range m.GetHunks() {
		if h.Accepted || !h.Rejected {
			t.Fatalf("expected hunk %d to be rejected", i)
		}
	}

	// Test Clear 'C'
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	m = newModel.(Model)
	for i, h := range m.GetHunks() {
		if h.Accepted || h.Rejected {
			t.Fatalf("expected hunk %d to be cleared", i)
		}
	}
}
