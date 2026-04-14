package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_StreamMessage_HeadersAndBodyShape(t *testing.T) {
	var (
		gotHeaders http.Header
		gotBody    []byte
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		gotBody = b

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	defer srv.Close()

	c := NewClient("test-key", "test-model")
	c.SetBaseURL(srv.URL)

	_, err := c.StreamMessage(context.Background(), StreamRequest{
		System: "sys",
		Messages: []Message{
			UserMessage("hi"),
			ToolResultMessage([]ToolResultBlock{
				{ToolUseID: "tu_1", Content: "ok", IsError: false},
			}),
			AssistantMessage([]ContentBlock{
				ToolUseBlock{ID: "tu_2", Name: "bash", Input: json.RawMessage(`{"command":"echo hi"}`)},
			}),
		},
		MaxTokens: 123,
	})
	if err != nil {
		t.Fatalf("StreamMessage error: %v", err)
	}

	if gotHeaders.Get("x-api-key") != "test-key" {
		t.Fatalf("missing/incorrect x-api-key header: %q", gotHeaders.Get("x-api-key"))
	}
	if gotHeaders.Get("anthropic-version") == "" {
		t.Fatalf("missing anthropic-version header")
	}
	if gotHeaders.Get("accept") != "text/event-stream" {
		t.Fatalf("accept header mismatch: %q", gotHeaders.Get("accept"))
	}
	if ct := gotHeaders.Get("content-type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type mismatch: %q", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("request body not json: %v\nbody=%s", err, string(gotBody))
	}
	if body["stream"] != true {
		t.Fatalf("expected stream=true, got %v", body["stream"])
	}
	if body["model"] != "test-model" {
		t.Fatalf("expected model=test-model, got %v", body["model"])
	}
	if int(body["max_tokens"].(float64)) != 123 {
		t.Fatalf("expected max_tokens=123, got %v", body["max_tokens"])
	}
	if body["system"] != "sys" {
		t.Fatalf("expected system=sys, got %v", body["system"])
	}
	if _, ok := body["messages"]; !ok {
		t.Fatalf("expected messages in request body")
	}
}

