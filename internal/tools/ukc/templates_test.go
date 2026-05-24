package ukc

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplatesCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "templates.json")

	// Load non-existent
	cache, err := NewTemplatesCache(path)
	if err != nil {
		t.Fatal(err)
	}

	// Set and Get
	err = cache.Set("rust", "uuid-123")
	if err != nil {
		t.Fatal(err)
	}
	val, ok := cache.Get("rust")
	if !ok || val != "uuid-123" {
		t.Errorf("expected uuid-123, got %v", val)
	}

	// Load existing
	cache2, err := NewTemplatesCache(path)
	if err != nil {
		t.Fatal(err)
	}
	val2, ok2 := cache2.Get("rust")
	if !ok2 || val2 != "uuid-123" {
		t.Errorf("expected uuid-123, got %v", val2)
	}
}

func TestBuildTemplateTool(t *testing.T) {
	tool := &BuildTemplate{}
	if tool.Name() != "ukc_build_template" {
		t.Errorf("unexpected name: %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("missing description")
	}
	if len(tool.InputSchema()) == 0 {
		t.Error("missing schema")
	}
	if !tool.NeedsPermission(nil) {
		t.Error("should need permission")
	}

	// Nil manager
	_, err := tool.Execute(context.Background(), []byte(`{"environment":"rust"}`))
	if err == nil {
		t.Error("expected error with nil manager")
	}

	tool.M = &Manager{}
	
	// Bad json
	_, err = tool.Execute(context.Background(), []byte(`{`))
	if err == nil {
		t.Error("expected error with bad json")
	}

	// Unknown environment
	_, err = tool.Execute(context.Background(), []byte(`{"environment":"unknown"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown environment") {
		t.Error("expected unknown environment error")
	}
}
