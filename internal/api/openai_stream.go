package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// newOpenAITranslatedStream reads OpenAI Chat Completions SSE and exposes the
// same StreamEvent types as the Anthropic Messages stream.
func newOpenAITranslatedStream(body io.ReadCloser) *Stream {
	pr, pw := io.Pipe()
	go func() {
		err := translateOpenAISSE(body, pw)
		_ = body.Close()
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()
	return newStream(pr)
}

type openaiChunk struct {
	Choices []struct {
		Delta struct {
			Content   *string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type oaToolAcc struct {
	blockIndex int
	started    bool
}

type oaTranslator struct {
	w          io.Writer
	textOpen   bool
	textIndex  int
	tools      map[int]*oaToolAcc
	nextIndex  int
	openBlocks map[int]bool
	usage      Usage
	stopReason string
	finished   bool
}

func newOATranslator(w io.Writer) *oaTranslator {
	return &oaTranslator{
		w:          w,
		tools:      map[int]*oaToolAcc{},
		openBlocks: map[int]bool{},
	}
}

func translateOpenAISSE(r io.Reader, w io.Writer) error {
	t := newOATranslator(w)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return t.finish()
		}
		if err := t.chunk(data); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return t.finish()
}

func (t *oaTranslator) chunk(data string) error {
	var ch openaiChunk
	if err := json.Unmarshal([]byte(data), &ch); err != nil {
		return fmt.Errorf("parse openai chunk: %w", err)
	}
	if ch.Error != nil && (ch.Error.Message != "" || ch.Error.Type != "") {
		typ := ch.Error.Type
		if typ == "" {
			typ = "error"
		}
		return fmt.Errorf("api error %s: %s", typ, ch.Error.Message)
	}
	if ch.Usage != nil {
		in := ch.Usage.PromptTokens
		if in == 0 {
			in = ch.Usage.InputTokens
		}
		out := ch.Usage.CompletionTokens
		if out == 0 {
			out = ch.Usage.OutputTokens
		}
		if in > 0 {
			t.usage.InputTokens = in
		}
		if out > 0 {
			t.usage.OutputTokens = out
		}
	}
	for _, choice := range ch.Choices {
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			if err := t.ensureText(); err != nil {
				return err
			}
			if err := t.emitDelta(t.textIndex, map[string]any{
				"type": "text_delta",
				"text": *choice.Delta.Content,
			}); err != nil {
				return err
			}
		}
		for _, tc := range choice.Delta.ToolCalls {
			if err := t.ensureTool(tc.Index, tc.ID, tc.Function.Name); err != nil {
				return err
			}
			if tc.Function.Arguments != "" {
				acc := t.tools[tc.Index]
				if err := t.emitDelta(acc.blockIndex, map[string]any{
					"type":         "input_json_delta",
					"partial_json": tc.Function.Arguments,
				}); err != nil {
					return err
				}
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			t.stopReason = mapOpenAIStopReason(*choice.FinishReason)
		}
	}
	return nil
}

func (t *oaTranslator) ensureText() error {
	if t.textOpen {
		return nil
	}
	idx := t.nextIndex
	t.nextIndex++
	t.textOpen = true
	t.textIndex = idx
	t.openBlocks[idx] = true
	return t.emit("content_block_start", map[string]any{
		"index":         idx,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
}

func (t *oaTranslator) ensureTool(openaiIndex int, id, name string) error {
	acc, ok := t.tools[openaiIndex]
	if !ok {
		acc = &oaToolAcc{blockIndex: t.nextIndex}
		t.nextIndex++
		t.tools[openaiIndex] = acc
	}
	if acc.started {
		return nil
	}
	if id == "" {
		id = fmt.Sprintf("call_%d", openaiIndex)
	}
	acc.started = true
	t.openBlocks[acc.blockIndex] = true
	return t.emit("content_block_start", map[string]any{
		"index": acc.blockIndex,
		"content_block": map[string]any{
			"type": "tool_use",
			"id":   id,
			"name": name,
		},
	})
}

func (t *oaTranslator) emitDelta(index int, delta map[string]any) error {
	return t.emit("content_block_delta", map[string]any{
		"index": index,
		"delta": delta,
	})
}

func (t *oaTranslator) finish() error {
	if t.finished {
		return nil
	}
	t.finished = true
	for i := 0; i < t.nextIndex; i++ {
		if t.openBlocks[i] {
			if err := t.emit("content_block_stop", map[string]any{"index": i}); err != nil {
				return err
			}
		}
	}
	if t.stopReason == "" {
		if len(t.tools) > 0 {
			t.stopReason = "tool_use"
		} else {
			t.stopReason = "end_turn"
		}
	}
	if err := t.emit("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": t.stopReason},
		"usage": map[string]any{
			"input_tokens":  t.usage.InputTokens,
			"output_tokens": t.usage.OutputTokens,
		},
	}); err != nil {
		return err
	}
	return t.emit("message_stop", map[string]any{})
}

func (t *oaTranslator) emit(event string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(t.w, "event: %s\ndata: %s\n\n", event, data)
	return err
}

func mapOpenAIStopReason(reason string) string {
	switch reason {
	case "tool_calls", "function_call":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}
