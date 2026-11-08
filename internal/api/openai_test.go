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

func TestProviderFromEnv(t *testing.T) {
	t.Setenv(EnvLLMProvider, "")
	t.Setenv(EnvOpenAIBaseURL, "")
	if p := ProviderFromEnv(); p != ProtocolAnthropic {
		t.Fatalf("default: got %q", p)
	}

	t.Setenv(EnvOpenAIBaseURL, "https://llm.drover.sh/v1")
	if p := ProviderFromEnv(); p != ProtocolOpenAI {
		t.Fatalf("base url: got %q", p)
	}

	t.Setenv(EnvLLMProvider, "anthropic")
	if p := ProviderFromEnv(); p != ProtocolAnthropic {
		t.Fatalf("explicit anthropic wins: got %q", p)
	}

	t.Setenv(EnvLLMProvider, "sglang")
	if p := ProviderFromEnv(); p != ProtocolOpenAI {
		t.Fatalf("sglang: got %q", p)
	}
}

func TestAPIKeyFromEnv_OpenAIFallsBackToSGLang(t *testing.T) {
	t.Setenv(EnvOpenAIAPIKey, "")
	t.Setenv(EnvSGLANGAPIKey, "  sgl-key  ")
	if g := APIKeyFromEnv(ProtocolOpenAI); g != "sgl-key" {
		t.Fatalf("got %q", g)
	}

	t.Setenv(EnvOpenAIAPIKey, "oa-key")
	if g := APIKeyFromEnv(ProtocolOpenAI); g != "oa-key" {
		t.Fatalf("openai key should win, got %q", g)
	}
}

func TestChatCompletionsURL(t *testing.T) {
	c := NewOpenAIClient("k", "m")
	c.SetBaseURL("https://llm.drover.sh/v1")
	if g := c.chatCompletionsURL(); g != "https://llm.drover.sh/v1/chat/completions" {
		t.Fatalf("got %q", g)
	}
	c.SetBaseURL("https://llm.drover.sh")
	if g := c.chatCompletionsURL(); g != "https://llm.drover.sh/v1/chat/completions" {
		t.Fatalf("host only: got %q", g)
	}
}

func TestMarshalOpenAIMessages_ToolsAndResults(t *testing.T) {
	msgs := marshalOpenAIMessages("sys", []Message{
		UserMessage("hi"),
		AssistantMessage([]ContentBlock{
			TextBlock{Text: "calling"},
			ToolUseBlock{ID: "call_1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)},
		}),
		ToolResultMessage([]ToolResultBlock{
			{ToolUseID: "call_1", Content: "ok", IsError: false},
		}),
	})
	if len(msgs) != 4 {
		t.Fatalf("expected system+user+assistant+tool, got %d: %#v", len(msgs), msgs)
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "sys" {
		t.Fatalf("system: %#v", msgs[0])
	}
	if msgs[2]["role"] != "assistant" {
		t.Fatalf("assistant role: %#v", msgs[2])
	}
	calls, ok := msgs[2]["tool_calls"].([]map[string]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls: %#v", msgs[2]["tool_calls"])
	}
	if msgs[3]["role"] != "tool" || msgs[3]["tool_call_id"] != "call_1" {
		t.Fatalf("tool result: %#v", msgs[3])
	}
}

func TestClient_StreamMessage_OpenAIHeadersAndBody(t *testing.T) {
	var (
		gotPath    string
		gotHeaders http.Header
		gotBody    []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewOpenAIClient("sgl-secret", "deepseek-v4-flash")
	c.SetBaseURL(srv.URL + "/v1")

	stream, err := c.StreamMessage(context.Background(), StreamRequest{
		System:    "sys",
		Messages:  []Message{UserMessage("ping")},
		MaxTokens: 32,
		Tools: []ToolDefinition{{
			Name:        "bash",
			Description: "run a command",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	defer stream.Close()
	for stream.Next() {
	}
	if stream.Err() != nil {
		t.Fatalf("stream err: %v", stream.Err())
	}

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if got := gotHeaders.Get("Authorization"); got != "Bearer sgl-secret" {
		t.Fatalf("Authorization = %q", got)
	}
	if gotHeaders.Get("x-api-key") != "" {
		t.Fatalf("should not send x-api-key for OpenAI protocol")
	}

	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["model"] != "deepseek-v4-flash" {
		t.Fatalf("model = %v", body["model"])
	}
	if body["stream"] != true {
		t.Fatalf("stream = %v", body["stream"])
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	t0 := tools[0].(map[string]any)
	if t0["type"] != "function" {
		t.Fatalf("tool type = %#v", t0)
	}
}

func TestTranslateOpenAISSE_TextAndTools(t *testing.T) {
	sse := strings.Join(byteLines(
		`data: {"choices":[{"delta":{"role":"assistant","content":"hi "}}]}`,
		`data: {"choices":[{"delta":{"content":"there"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":\"ls\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`,
		`data: [DONE]`,
	), "\n")

	st := newOpenAITranslatedStream(io.NopCloser(strings.NewReader(sse)))
	defer st.Close()

	var texts []string
	var tools []ToolUseBlock
	var delta MessageDeltaEvent
	for st.Next() {
		switch e := st.Event().(type) {
		case ContentBlockStartEvent:
			if tu, ok := e.ContentBlock.(ToolUseBlock); ok {
				tools = append(tools, tu)
			}
		case ContentBlockDeltaEvent:
			if td, ok := e.Delta.(TextDelta); ok {
				texts = append(texts, td.Text)
			}
			if jd, ok := e.Delta.(InputJSONDelta); ok {
				if len(tools) == 0 {
					t.Fatal("json delta before tool start")
				}
				tools[len(tools)-1].Input = json.RawMessage(append([]byte(tools[len(tools)-1].Input), jd.PartialJSON...))
			}
		case MessageDeltaEvent:
			delta = e
		}
	}
	if st.Err() != nil {
		t.Fatalf("stream: %v", st.Err())
	}
	if strings.Join(texts, "") != "hi there" {
		t.Fatalf("text = %q", texts)
	}
	if len(tools) != 1 || tools[0].ID != "call_1" || tools[0].Name != "bash" {
		t.Fatalf("tools = %#v", tools)
	}
	if string(tools[0].Input) != `{"command":"ls"}` {
		t.Fatalf("input = %s", tools[0].Input)
	}
	if delta.StopReason != "tool_use" {
		t.Fatalf("stop = %q", delta.StopReason)
	}
	if delta.InputTokens != 11 || delta.OutputTokens != 7 {
		t.Fatalf("usage in=%d out=%d", delta.InputTokens, delta.OutputTokens)
	}
}

func TestNewClientFromEnv_OpenAI(t *testing.T) {
	t.Setenv(EnvLLMProvider, "openai")
	t.Setenv(EnvOpenAIBaseURL, "https://llm.drover.sh/v1")
	t.Setenv(EnvOpenAIAPIKey, "")
	t.Setenv(EnvSGLANGAPIKey, "k")
	c := NewClientFromEnv("deepseek-v4-flash")
	if c.Protocol() != ProtocolOpenAI {
		t.Fatalf("protocol = %q", c.Protocol())
	}
	if c.baseURL != "https://llm.drover.sh/v1" {
		t.Fatalf("base = %q", c.baseURL)
	}
	if c.apiKey != "k" {
		t.Fatalf("key = %q", c.apiKey)
	}
}

func byteLines(lines ...string) []string {
	return lines
}
