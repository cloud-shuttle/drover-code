package ukc

import (
	"bytes"
	"testing"
)

func FuzzParseExecStream(f *testing.F) {
	// Add some valid seed corpus
	f.Add([]byte("data: {\"stream\":\"stdout\",\"line\":\"hello\"}\n\ndata: {\"done\":true,\"exit_code\":0}\n\n"))
	f.Add([]byte("data: {\"stream\":\"stderr\",\"line\":\"warning\"}\n\ndata: {\"done\":true,\"exit_code\":1}\n\n"))
	f.Add([]byte("data: {\"stream\":\"stdout\",\"line\":\"\"}\n\ndata: {\"done\":true,\"exit_code\":127}\n\n"))
	f.Add([]byte("invalid json"))
	f.Add([]byte("data: invalid json"))

	f.Fuzz(func(t *testing.T, data []byte) {
		ParseExecStream(bytes.NewReader(data))
	})
}
