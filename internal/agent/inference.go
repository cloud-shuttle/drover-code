package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/telemetry"
)

// SemanticEvent represents a high-level intent or chunk yielded by the inference engine.
type SemanticEvent interface {
	isSemanticEvent()
}

type TextDeltaYielded struct {
	Text string
}

func (TextDeltaYielded) isSemanticEvent() {}

type TextYielded struct {
	Text string
}

func (TextYielded) isSemanticEvent() {}

type ToolCallRequested struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolCallRequested) isSemanticEvent() {}

type TurnCompleted struct {
	StopReason string
	Usage      api.Usage
}

func (TurnCompleted) isSemanticEvent() {}

type InferenceError struct {
	Err error
}

func (InferenceError) isSemanticEvent() {}

// InferenceDriver abstracts the underlying LLM provider mechanics (e.g. SSE chunking, 429s).
type InferenceDriver interface {
	// Generate takes the current conversation state and tool definitions, and streams semantic events.
	Generate(ctx context.Context, convo *convo.Manager, tools []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error)
	// Model returns the identifier of the underlying model.
	Model() string
}

type AnthropicInferenceDriver struct {
	client *api.Client
}

func NewAnthropicInferenceDriver(client *api.Client) *AnthropicInferenceDriver {
	return &AnthropicInferenceDriver{client: client}
}

func (d *AnthropicInferenceDriver) Model() string {
	return d.client.Model()
}

func (d *AnthropicInferenceDriver) Generate(ctx context.Context, convoMgr *convo.Manager, tools []api.ToolDefinition) (<-chan SemanticEvent, telemetry.SpanID, error) {
	sys := convoMgr.SystemPrompt()
	msgs := convoMgr.Messages()
	req := api.StreamRequest{
		System:    sys,
		Messages:  msgs,
		Tools:     tools,
		MaxTokens: 8096, // Or configurable
	}

	tracer := telemetry.TracerFrom(ctx)
	traceID := telemetry.TraceIDFrom(ctx)
	genID := tracer.StartGeneration(telemetry.GenerationParams{
		TraceID:   traceID,
		Name:      "stream-semantic-events",
		Model:     d.client.Model(),
		Input:     msgs,
		System:    sys,
		MaxTokens: req.MaxTokens,
	})

	stream, err := d.client.StreamMessage(ctx, req)
	if err != nil {
		return nil, genID, fmt.Errorf("stream message: %w", err)
	}

	ch := make(chan SemanticEvent, 32)
	go func() {
		defer close(ch)
		defer stream.Close()

		type textAcc struct {
			buf strings.Builder
		}
		type toolAcc struct {
			id      string
			name    string
			jsonBuf strings.Builder
		}

		textAccs := map[int]*textAcc{}
		toolAccs := map[int]*toolAcc{}

		var usage api.Usage
		var stopReason string
		var out strings.Builder

		for stream.Next() {
			switch e := stream.Event().(type) {
			case api.ContentBlockStartEvent:
				switch b := e.ContentBlock.(type) {
				case api.TextBlock:
					_ = b
					textAccs[e.Index] = &textAcc{}
				case api.ToolUseBlock:
					toolAccs[e.Index] = &toolAcc{id: b.ID, name: b.Name}
				}

			case api.ContentBlockDeltaEvent:
				switch delta := e.Delta.(type) {
				case api.TextDelta:
					if acc, ok := textAccs[e.Index]; ok {
						acc.buf.WriteString(delta.Text)
					}
					ch <- TextDeltaYielded{Text: delta.Text}

				case api.InputJSONDelta:
					if acc, ok := toolAccs[e.Index]; ok {
						acc.jsonBuf.WriteString(delta.PartialJSON)
					}
				}

			case api.ContentBlockStopEvent:
				if acc, ok := textAccs[e.Index]; ok {
					text := acc.buf.String()
					out.WriteString(text)
					ch <- TextYielded{Text: text}
					delete(textAccs, e.Index)
				}
				if acc, ok := toolAccs[e.Index]; ok {
					raw := acc.jsonBuf.String()
					if raw == "" {
						raw = "{}"
					}
					ch <- ToolCallRequested{
						ID:    acc.id,
						Name:  acc.name,
						Input: json.RawMessage(raw),
					}
					delete(toolAccs, e.Index)
				}

			case api.MessageDeltaEvent:
				usage.InputTokens = e.InputTokens
				usage.OutputTokens = e.OutputTokens
				stopReason = e.StopReason
			}
		}

		if err := stream.Err(); err != nil {
			tracer.EndGeneration(genID, telemetry.GenerationResult{
				Error: err,
			})
			ch <- InferenceError{Err: err}
			return
		}

		ch <- TurnCompleted{
			StopReason: stopReason,
			Usage:      usage,
		}

		tracer.EndGeneration(genID, telemetry.GenerationResult{
			Output:       out.String(),
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			StopReason:   stopReason,
		})
	}()

	return ch, genID, nil
}
