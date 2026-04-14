package convo

import (
	"bytes"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
)

// FuzzConvoManager exercises Manager under random UTF-8-ish payloads split on NUL.
// Invariant: no panics; EstimatedTokens >= 0; Reset leaves an empty history.
func FuzzConvoManager(f *testing.F) {
	f.Add([]byte("sys\x00msg1\x00msg2\x00tail"))
	f.Add([]byte{0, 0xff, 0xfe})

	f.Fuzz(func(t *testing.T, data []byte) {
		const max = 48 * 1024
		if len(data) > max {
			data = data[:max]
		}
		parts := bytes.Split(data, []byte{0})
		var sys string
		if len(parts) > 0 {
			sys = string(parts[0])
		}
		m := NewManagerWithSystem(sys)
		const maxMsg = 256
		for i := 1; i < len(parts) && i <= maxMsg; i++ {
			m.Append(api.UserMessage(string(parts[i])))
		}
		if m.EstimatedTokens() < 0 {
			t.Fatal("negative EstimatedTokens")
		}
		limit := (len(data) % 50_000) + 1
		m.SetContextLimit(limit)
		_ = m.NeedsCompaction()
		keep := 0
		if len(data) > 0 {
			keep = int(data[0]) % 48
		}
		summary := "s"
		if len(parts) > 1 {
			summary = string(parts[len(parts)-1])
		}
		m.Summarise(summary, keep)
		_ = m.Messages()
		m.Reset()
		if len(m.Messages()) != 0 {
			t.Fatal("Reset did not clear messages")
		}
	})
}
