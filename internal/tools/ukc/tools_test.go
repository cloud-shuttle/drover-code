package ukc

import (
	"context"
	"strings"
	"testing"
)

func TestCreateTool(t *testing.T) {
	tool := &Create{M: nil}
	if tool.Name() != "ukc_create" {
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
	_, err := tool.Execute(context.Background(), []byte(`{"name":"test"}`))
	if err == nil {
		t.Error("expected error with nil manager")
	}

	// Bad input
	tool.M = &Manager{}
	_, err = tool.Execute(context.Background(), []byte(`{bad`))
	if err == nil {
		t.Error("expected error with bad json")
	}

	// Missing name
	_, err = tool.Execute(context.Background(), []byte(`{"name":""}`))
	if err == nil {
		t.Error("expected error with empty name")
	}

	// With templates, missing template
	tool.M.Templates, _ = NewTemplatesCache(t.TempDir() + "/t.json")
	_, err = tool.Execute(context.Background(), []byte(`{"name":"foo", "environment":"rust"}`))
	if err == nil || !strings.Contains(err.Error(), "template for environment") {
		t.Errorf("expected template not found error, got %v", err)
	}
}

func TestDeleteTool(t *testing.T) {
	tool := &Delete{M: nil}
	if tool.Name() != "ukc_delete" {
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
	_, err := tool.Execute(context.Background(), []byte(`{"instance_id":"test"}`))
	if err == nil {
		t.Error("expected error with nil manager")
	}

	tool.M = &Manager{}
	// Missing id
	_, err = tool.Execute(context.Background(), []byte(`{"instance_id":""}`))
	if err == nil {
		t.Error("expected error with empty id")
	}
}

func TestDeleteAllTool(t *testing.T) {
	tool := &DeleteAll{M: nil}
	if tool.Name() != "ukc_delete_all" {
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
	_, err := tool.Execute(context.Background(), []byte(`{}`))
	if err == nil {
		t.Error("expected error with nil manager")
	}
}

func TestExecTool(t *testing.T) {
	tool := &Exec{M: nil}
	if tool.Name() != "ukc_exec" {
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
	_, err := tool.Execute(context.Background(), []byte(`{"instance_id":"i","command":"c"}`))
	if err == nil {
		t.Error("expected error with nil manager")
	}

	tool.M = &Manager{}
	// Missing id
	_, err = tool.Execute(context.Background(), []byte(`{"command":"c"}`))
	if err == nil {
		t.Error("expected error with empty id")
	}
	// Missing command
	_, err = tool.Execute(context.Background(), []byte(`{"instance_id":"i"}`))
	if err == nil {
		t.Error("expected error with empty command")
	}
	// Missing timeout
	_, err = tool.Execute(context.Background(), []byte(`{"instance_id":"i","command":"c"}`))
	if err == nil || !strings.Contains(err.Error(), "timeout_seconds must be positive") {
		t.Errorf("expected timeout error, got %v", err)
	}

	// Unknown instance
	_, err = tool.Execute(context.Background(), []byte(`{"instance_id":"i","command":"c","timeout_seconds":10}`))
	if err == nil || !strings.Contains(err.Error(), "unknown instance_id") {
		t.Error("expected unknown instance error")
	}
}

func TestListTool(t *testing.T) {
	tool := &List{M: nil}
	if tool.Name() != "ukc_list" {
		t.Errorf("unexpected name: %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("missing description")
	}
	if len(tool.InputSchema()) == 0 {
		t.Error("missing schema")
	}
	if tool.NeedsPermission(nil) {
		t.Error("should not need permission")
	}

	// Nil manager
	_, err := tool.Execute(context.Background(), []byte(`{}`))
	if err == nil {
		t.Error("expected error with nil manager")
	}

	tool.M = &Manager{}
	out, err := tool.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Error("unexpected error")
	}
	if !strings.Contains(out, "no instances in registry") {
		t.Errorf("unexpected output: %s", out)
	}
}
