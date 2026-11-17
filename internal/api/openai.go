package api

import (
	"encoding/json"
	"strings"
)

const defaultOpenAIBaseURL = "https://api.openai.com"

func (c *Client) chatCompletionsURL() string {
	base := strings.TrimRight(c.baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

// openAIMaxTokenField picks the limit field the model accepts.
// gpt-5, gpt-6, and the o-series reject max_tokens. Other Chat Completions
// servers, including SGLang, still use max_tokens.
func openAIModelID(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return id
}

func openAIMaxTokenField(model string) string {
	id := openAIModelID(model)
	if strings.HasPrefix(id, "gpt-5") || strings.HasPrefix(id, "gpt-6") ||
		strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") || strings.HasPrefix(id, "o4") {
		return "max_completion_tokens"
	}
	return "max_tokens"
}

// gpt-6 chat completions reject function tools. The Responses API accepts them.
func openAIUsesResponses(model string) bool {
	return strings.HasPrefix(openAIModelID(model), "gpt-6")
}

func buildOpenAIRequestBody(model string, req StreamRequest) ([]byte, error) {
	body := map[string]any{
		"model":                    model,
		openAIMaxTokenField(model): req.MaxTokens,
		"stream":                   true,
		"messages":                 marshalOpenAIMessages(req.System, req.Messages),
	}
	if len(req.Tools) > 0 {
		body["tools"] = marshalOpenAITools(req.Tools)
		body["tool_choice"] = "auto"
	}
	return json.Marshal(body)
}

func (c *Client) responsesURL() string {
	base := strings.TrimRight(c.baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/responses"
	}
	return base + "/v1/responses"
}

func buildResponsesRequestBody(model string, req StreamRequest) ([]byte, error) {
	body := map[string]any{
		"model":             model,
		"max_output_tokens": req.MaxTokens,
		"stream":            true,
		"input":             marshalResponsesInput(req.Messages),
	}
	if req.System != "" {
		body["instructions"] = req.System
	}
	if len(req.Tools) > 0 {
		body["tools"] = marshalResponsesTools(req.Tools)
	}
	return json.Marshal(body)
}

func marshalResponsesTools(tools []ToolDefinition) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		item := map[string]any{
			"type":        "function",
			"name":        t.Name,
			"description": t.Description,
		}
		if len(t.InputSchema) > 0 {
			var params any
			if err := json.Unmarshal(t.InputSchema, &params); err == nil {
				item["parameters"] = params
			}
		}
		out = append(out, item)
	}
	return out
}

func marshalResponsesInput(msgs []Message) []any {
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == RoleAssistant {
			out = append(out, marshalResponsesAssistant(m)...)
			continue
		}
		out = append(out, marshalResponsesUser(m)...)
	}
	return out
}

func marshalResponsesAssistant(m Message) []any {
	var text strings.Builder
	var calls []any
	for _, block := range m.Content {
		switch b := block.(type) {
		case TextBlock:
			text.WriteString(b.Text)
		case ToolUseBlock:
			args := string(b.Input)
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			calls = append(calls, map[string]any{
				"type":      "function_call",
				"call_id":   b.ID,
				"name":      b.Name,
				"arguments": args,
			})
		}
	}
	out := make([]any, 0, 1+len(calls))
	if text.Len() > 0 {
		out = append(out, map[string]any{"role": "assistant", "content": text.String()})
	}
	out = append(out, calls...)
	return out
}

func marshalResponsesUser(m Message) []any {
	var text strings.Builder
	var results []ToolResultBlock
	for _, block := range m.Content {
		switch b := block.(type) {
		case TextBlock:
			text.WriteString(b.Text)
		case ToolResultBlock:
			results = append(results, b)
		}
	}
	// Tool output comes first. A resumed prompt is another block on the same
	// user turn, and it must not sit between a call and its result.
	out := make([]any, 0, 1+len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"type":    "function_call_output",
			"call_id": r.ToolUseID,
			"output":  responsesOutput(r),
		})
	}
	if text.Len() > 0 || len(results) == 0 {
		out = append(out, map[string]any{"role": "user", "content": text.String()})
	}
	return out
}

func marshalOpenAITools(tools []ToolDefinition) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		fn := map[string]any{
			"name":        t.Name,
			"description": t.Description,
		}
		if len(t.InputSchema) > 0 {
			var params any
			if err := json.Unmarshal(t.InputSchema, &params); err == nil {
				fn["parameters"] = params
			}
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": fn,
		})
	}
	return out
}

func marshalOpenAIMessages(system string, msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs)+1)
	if system != "" {
		out = append(out, map[string]any{
			"role":    "system",
			"content": system,
		})
	}
	for _, m := range msgs {
		if m.Role == RoleAssistant {
			out = append(out, marshalOpenAIAssistant(m))
			continue
		}
		out = append(out, marshalOpenAIUserOrTool(m)...)
	}
	return out
}

func marshalOpenAIAssistant(m Message) map[string]any {
	var text strings.Builder
	var calls []map[string]any
	for _, block := range m.Content {
		switch b := block.(type) {
		case TextBlock:
			text.WriteString(b.Text)
		case ToolUseBlock:
			args := string(b.Input)
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			calls = append(calls, map[string]any{
				"id":   b.ID,
				"type": "function",
				"function": map[string]any{
					"name":      b.Name,
					"arguments": args,
				},
			})
		}
	}
	msg := map[string]any{"role": "assistant"}
	if text.Len() > 0 {
		msg["content"] = text.String()
	} else if len(calls) > 0 {
		msg["content"] = nil
	} else {
		msg["content"] = ""
	}
	if len(calls) > 0 {
		msg["tool_calls"] = calls
	}
	return msg
}

func marshalOpenAIUserOrTool(m Message) []map[string]any {
	var text strings.Builder
	var results []ToolResultBlock
	for _, block := range m.Content {
		switch b := block.(type) {
		case TextBlock:
			text.WriteString(b.Text)
		case ToolResultBlock:
			results = append(results, b)
		}
	}
	// Tool output comes first. A resumed prompt is another block on the same
	// user turn, and it must not sit between a call and its result.
	out := make([]map[string]any, 0, 1+len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"role":         "tool",
			"tool_call_id": r.ToolUseID,
			"content":      chatToolContent(r),
		})
	}
	if text.Len() > 0 || len(results) == 0 {
		out = append(out, map[string]any{
			"role":    "user",
			"content": text.String(),
		})
	}
	return out
}
