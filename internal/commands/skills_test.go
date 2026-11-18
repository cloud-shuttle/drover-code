package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkills(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".drover", "skills", "grill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: grill\ndescription: Grill the plan\n---\n\n# Grill\n\nAsk questions.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLoader(dir)
	reg, err := l.LoadSkills()
	if err != nil {
		t.Fatal(err)
	}
	s, ok := reg.Get("grill")
	if !ok {
		t.Fatal("grill not loaded")
	}
	if s.Description != "Grill the plan" {
		t.Fatalf("desc=%q", s.Description)
	}
	cmd := s.AsCommand()
	if cmd.Name != "skill:grill" {
		t.Fatalf("cmd name %s", cmd.Name)
	}
}
