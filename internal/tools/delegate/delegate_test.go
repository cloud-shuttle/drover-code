package delegate

import (
	"context"
	"strings"
	"testing"
)

func TestAgent_ReturnsAskResult(t *testing.T) {
	a := &Agent{Ask: func(context.Context, string) (string, error) {
		return "wrote note.txt", nil
	}}
	out, err := a.Execute(context.Background(), []byte(`{"task":"add a note"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "wrote note.txt" {
		t.Fatalf("out %q", out)
	}
	if !a.NeedsPermission(nil) {
		t.Fatal("delegate must ask permission")
	}
}

func TestAgent_NilAsk(t *testing.T) {
	_, err := (&Agent{}).Execute(context.Background(), []byte(`{"task":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatal(err)
	}
}

func TestAgent_EmptyTask(t *testing.T) {
	a := &Agent{Ask: func(context.Context, string) (string, error) { return "no", nil }}
	_, err := a.Execute(context.Background(), []byte(`{"task":"  "}`))
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatal(err)
	}
}
