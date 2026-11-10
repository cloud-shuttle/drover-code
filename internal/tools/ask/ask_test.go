package ask

import (
	"context"
	"strings"
	"testing"
)

func TestUser_ReturnsAnswer(t *testing.T) {
	tool := &User{Ask: func(_ context.Context, question string) (string, error) {
		if question != "Ship it?" {
			t.Fatalf("question %q", question)
		}
		return "  yes  ", nil
	}}
	out, err := tool.Execute(context.Background(), []byte(`{"question":"  Ship it?  "}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "yes" {
		t.Fatalf("out %q", out)
	}
	if tool.NeedsPermission(nil) {
		t.Fatal("ask_user needs permission")
	}
}

func TestUser_NilAsk(t *testing.T) {
	_, err := (&User{}).Execute(context.Background(), []byte(`{"question":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatal(err)
	}
}

func TestUser_EmptyQuestion(t *testing.T) {
	tool := &User{Ask: func(context.Context, string) (string, error) {
		t.Fatal("Ask called")
		return "", nil
	}}
	_, err := tool.Execute(context.Background(), []byte(`{"question":"  "}`))
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatal(err)
	}
}

func TestUser_AskError(t *testing.T) {
	tool := &User{Ask: func(context.Context, string) (string, error) {
		return "", context.Canceled
	}}
	_, err := tool.Execute(context.Background(), []byte(`{"question":"continue?"}`))
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatal(err)
	}
}
