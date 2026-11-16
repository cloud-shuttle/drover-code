package explore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAgent_ReturnsAskResult(t *testing.T) {
	tool := &Agent{Ask: func(_ context.Context, task string) (string, error) {
		if task != "where is the loop" {
			t.Fatalf("task %q", task)
		}
		return "internal/agent/loop.go — the run loop", nil
	}}
	out, err := tool.Execute(context.Background(), []byte(`{"task":"  where is the loop  "}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "internal/agent/loop.go — the run loop" {
		t.Fatalf("out %q", out)
	}
	if tool.NeedsPermission(nil) {
		t.Fatal("explore needs permission")
	}
}

func TestAgent_NilAsk(t *testing.T) {
	_, err := (&Agent{}).Execute(context.Background(), []byte(`{"task":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatal(err)
	}
}

func TestAgent_RejectsEmptyTask(t *testing.T) {
	tool := &Agent{Ask: func(context.Context, string) (string, error) {
		t.Fatal("Ask called")
		return "", nil
	}}
	_, err := tool.Execute(context.Background(), []byte(`{"task":"   "}`))
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatal(err)
	}
}

func TestAgent_AskError(t *testing.T) {
	tool := &Agent{Ask: func(context.Context, string) (string, error) {
		return "", errors.New("stopped after 8 turns")
	}}
	_, err := tool.Execute(context.Background(), []byte(`{"task":"find it"}`))
	if err == nil || !strings.Contains(err.Error(), "stopped after 8 turns") {
		t.Fatal(err)
	}
}
