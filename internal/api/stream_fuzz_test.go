package api

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func FuzzStreamNext(f *testing.F) {
	seed := strings.Join([]string{
		"event: ping",
		"data: {}",
		"",
		"event: content_block_start",
		`data: {"index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: message_stop",
		"data: {}",
		"",
	}, "\n")
	f.Add([]byte(seed))

	f.Fuzz(func(t *testing.T, data []byte) {
		st := newStream(io.NopCloser(bytes.NewReader(data)))
		defer st.Close()

		const maxSteps = 50_000
		for i := 0; i < maxSteps && st.Next(); i++ {
			_ = st.Event()
		}
		_ = st.Err()
	})
}
