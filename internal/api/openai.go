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

func buildOpenAIRequestBody(model string, req StreamRequest) ([]byte, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
		"messages":   marshalOpenAIMessages(req.System, req.Messages),
	}
	if len(req.Tools) > 0 {
		body["tools"] = marshalOpenAITools(req.Tools)
		body["tool_choice"] = "auto"
	}
	return json.Marshal(body)
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
	out := make([]map[string]any, 0, 1+len(results))
	if text.Len() > 0 || len(results) == 0 {
		out = append(out, map[string]any{
			"role":    "user",
			"content": text.String(),
		})
	}
	for _, r := range results {
		content := r.Content
		if r.IsError && content != "" && !strings.HasPrefix(strings.ToLower(content), "error") {
			content = "Error: " + content
		}
		out = append(out, map[string]any{
			"role":         "tool",
			"tool_call_id": r.ToolUseID,
			"content":      content,
		})
	}
	return out
}
