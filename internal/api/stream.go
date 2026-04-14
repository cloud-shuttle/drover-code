package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Stream reads a server-sent event stream from the Anthropic API and
// presents it as a typed iterator.
//
// Usage:
//
//	stream, err := client.StreamMessage(ctx, req)
//	if err != nil { ... }
//	defer stream.Close()
//
//	for stream.Next() {
//	    switch e := stream.Event().(type) {
//	    case api.ContentBlockDeltaEvent: ...
//	    }
//	}
//	if err := stream.Err(); err != nil { ... }
type Stream struct {
	scanner *bufio.Scanner
	body    io.Closer
	current StreamEvent
	err     error
	done    bool
}

func newStream(body io.ReadCloser) *Stream {
	sc := bufio.NewScanner(body)
	// Increase the buffer size — tool inputs can be large.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &Stream{scanner: sc, body: body}
}

// Next advances the stream to the next meaningful event.
// Returns false when the stream ends (message_stop) or an error occurs.
func (s *Stream) Next() bool {
	if s.done || s.err != nil {
		return false
	}

	for {
		eventType, data, err := s.readBlock()
		if err != nil {
			if err == io.EOF {
				s.done = true
				return false
			}
			s.err = err
			return false
		}

		ev, err := parseEvent(eventType, data)
		if err != nil {
			s.err = err
			return false
		}

		// nil means "skip this event" (ping, message_start, unknown types)
		if ev == nil {
			continue
		}

		// message_stop is the terminal marker; stop the iterator cleanly.
		if _, ok := ev.(MessageStopEvent); ok {
			s.done = true
			return false
		}

		s.current = ev
		return true
	}
}

// Event returns the current event. Only valid after a successful Next() call.
func (s *Stream) Event() StreamEvent { return s.current }

// Err returns the first error encountered, if any.
func (s *Stream) Err() error { return s.err }

// Close releases the underlying HTTP response body.
func (s *Stream) Close() { s.body.Close() }

// readBlock scans lines until a blank line, collecting the event type and data.
// SSE format: each "block" is terminated by a blank line.
func (s *Stream) readBlock() (eventType, data string, err error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()

		if line == "" {
			// Blank line = end of this event block.
			if data != "" || eventType != "" {
				return eventType, data, nil
			}
			// Empty block (consecutive blank lines) — keep scanning.
			continue
		}

		if after, ok := strings.CutPrefix(line, "event: "); ok {
			eventType = after
		} else if after, ok := strings.CutPrefix(line, "data: "); ok {
			data = after
		}
		// Lines starting with ':' are SSE comments; ignore them.
	}

	if scanErr := s.scanner.Err(); scanErr != nil {
		return "", "", scanErr
	}
	return "", "", io.EOF
}

// ----------------------------------------------------------------------------
// Wire-format types — private to this file
// ----------------------------------------------------------------------------

type wireContentBlockStart struct {
	Index        int             `json:"index"`
	ContentBlock json.RawMessage `json:"content_block"`
}

type wireContentBlock struct {
	Type string `json:"type"`
	// text block fields
	Text string `json:"text"`
	// tool_use block fields
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type wireContentBlockDelta struct {
	Index int             `json:"index"`
	Delta json.RawMessage `json:"delta"`
}

type wireDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`         // text_delta
	PartialJSON string `json:"partial_json"` // input_json_delta
}

type wireContentBlockStop struct {
	Index int `json:"index"`
}

type wireMessageDelta struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type wireError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseEvent converts a raw SSE event into a typed StreamEvent.
// Returns nil for events we can safely ignore (ping, message_start, etc.).
// Returns an error for malformed data or an API-level error event.
func parseEvent(eventType, data string) (StreamEvent, error) {
	switch eventType {
	case "ping", "message_start", "":
		// message_start carries initial metadata we don't need right now.
		// We get token counts from message_delta instead.
		return nil, nil

	case "content_block_start":
		var w wireContentBlockStart
		if err := json.Unmarshal([]byte(data), &w); err != nil {
			return nil, fmt.Errorf("parse content_block_start: %w", err)
		}

		var cb wireContentBlock
		if err := json.Unmarshal(w.ContentBlock, &cb); err != nil {
			return nil, fmt.Errorf("parse content block: %w", err)
		}

		var block ContentBlock
		switch cb.Type {
		case "text":
			block = TextBlock{Text: cb.Text}
		case "tool_use":
			block = ToolUseBlock{ID: cb.ID, Name: cb.Name}
		default:
			return nil, nil // unknown block type — future-proof skip
		}

		return ContentBlockStartEvent{Index: w.Index, ContentBlock: block}, nil

	case "content_block_delta":
		var w wireContentBlockDelta
		if err := json.Unmarshal([]byte(data), &w); err != nil {
			return nil, fmt.Errorf("parse content_block_delta: %w", err)
		}

		var d wireDelta
		if err := json.Unmarshal(w.Delta, &d); err != nil {
			return nil, fmt.Errorf("parse delta: %w", err)
		}

		var delta Delta
		switch d.Type {
		case "text_delta":
			delta = TextDelta{Text: d.Text}
		case "input_json_delta":
			delta = InputJSONDelta{PartialJSON: d.PartialJSON}
		default:
			return nil, nil
		}

		return ContentBlockDeltaEvent{Index: w.Index, Delta: delta}, nil

	case "content_block_stop":
		var w wireContentBlockStop
		if err := json.Unmarshal([]byte(data), &w); err != nil {
			return nil, fmt.Errorf("parse content_block_stop: %w", err)
		}
		return ContentBlockStopEvent{Index: w.Index}, nil

	case "message_delta":
		var w wireMessageDelta
		if err := json.Unmarshal([]byte(data), &w); err != nil {
			return nil, fmt.Errorf("parse message_delta: %w", err)
		}
		return MessageDeltaEvent{
			StopReason:   w.Delta.StopReason,
			InputTokens:  w.Usage.InputTokens,
			OutputTokens: w.Usage.OutputTokens,
		}, nil

	case "message_stop":
		return MessageStopEvent{}, nil

	case "error":
		var w wireError
		if err := json.Unmarshal([]byte(data), &w); err != nil {
			return nil, fmt.Errorf("parse error event: %w", err)
		}
		return nil, fmt.Errorf("api error %s: %s", w.Error.Type, w.Error.Message)

	default:
		return nil, nil // forward-compatible: ignore unknown event types
	}
}

