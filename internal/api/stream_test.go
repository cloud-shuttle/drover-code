package api

import (
	"io"
	"strings"
	"testing"
)

func TestStream_TextEvents(t *testing.T) {
	sse := strings.Join([]string{
		"event: ping",
		"data: {}",
		"",
		"event: message_start",
		"data: {}",
		"",
		"event: content_block_start",
		`data: {"index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"index":0,"delta":{"type":"text_delta","text":"hello "}}`,
		"",
		"event: content_block_delta",
		`data: {"index":0,"delta":{"type":"text_delta","text":"world"}}`,
		"",
		"event: content_block_stop",
		`data: {"index":0}`,
		"",
		"event: message_delta",
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":3,"output_tokens":2}}`,
		"",
		"event: message_stop",
		"data: {}",
		"",
	}, "\n")

	st := newStream(io.NopCloser(strings.NewReader(sse)))
	defer st.Close()

	var got []StreamEvent
	for st.Next() {
		got = append(got, st.Event())
	}
	if st.Err() != nil {
		t.Fatalf("unexpected stream error: %v", st.Err())
	}

	if len(got) != 5 {
		t.Fatalf("expected 5 events, got %d (%T)", len(got), got)
	}
	if _, ok := got[0].(ContentBlockStartEvent); !ok {
		t.Fatalf("event[0] expected ContentBlockStartEvent, got %T", got[0])
	}
	if e, ok := got[1].(ContentBlockDeltaEvent); !ok {
		t.Fatalf("event[1] expected ContentBlockDeltaEvent, got %T", got[1])
	} else if _, ok := e.Delta.(TextDelta); !ok {
		t.Fatalf("event[1] expected TextDelta, got %T", e.Delta)
	}
	if e, ok := got[2].(ContentBlockDeltaEvent); !ok {
		t.Fatalf("event[2] expected ContentBlockDeltaEvent, got %T", got[2])
	} else if _, ok := e.Delta.(TextDelta); !ok {
		t.Fatalf("event[2] expected TextDelta, got %T", e.Delta)
	}
	if e, ok := got[3].(ContentBlockStopEvent); !ok {
		t.Fatalf("event[3] expected ContentBlockStopEvent, got %T", got[3])
	} else if e.Index != 0 {
		t.Fatalf("expected stop index 0, got %d", e.Index)
	}
	if e, ok := got[4].(MessageDeltaEvent); !ok {
		t.Fatalf("event[4] expected MessageDeltaEvent, got %T", got[4])
	} else {
		if e.InputTokens != 3 || e.OutputTokens != 2 {
			t.Fatalf("unexpected usage: in=%d out=%d", e.InputTokens, e.OutputTokens)
		}
	}
}

func TestStream_ToolJSONDeltas(t *testing.T) {
	sse := strings.Join([]string{
		"event: content_block_start",
		`data: {"index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"bash","input":{}}}`,
		"",
		"event: content_block_delta",
		`data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"echo "}}`,
		"",
		"event: content_block_delta",
		`data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"hi\"}"}}`,
		"",
		"event: content_block_stop",
		`data: {"index":0}`,
		"",
		"event: message_stop",
		"data: {}",
		"",
	}, "\n")

	st := newStream(io.NopCloser(strings.NewReader(sse)))
	defer st.Close()

	var partials []string
	for st.Next() {
		if e, ok := st.Event().(ContentBlockDeltaEvent); ok {
			if d, ok := e.Delta.(InputJSONDelta); ok {
				partials = append(partials, d.PartialJSON)
			}
		}
	}
	if st.Err() != nil {
		t.Fatalf("unexpected stream error: %v", st.Err())
	}
	got := strings.Join(partials, "")
	want := `{"command":"echo hi"}`
	if got != want {
		t.Fatalf("partial json mismatch:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestStream_ErrorEvent(t *testing.T) {
	sse := "event: error\n" +
		`data: {"error":{"type":"rate_limit_error","message":"nope"}}` + "\n\n"
	st := newStream(io.NopCloser(strings.NewReader(sse)))
	defer st.Close()

	for st.Next() {
	}
	if st.Err() == nil {
		t.Fatalf("expected non-nil Err()")
	}
	if !strings.Contains(st.Err().Error(), "nope") {
		t.Fatalf("expected error to contain message, got: %v", st.Err())
	}
}

