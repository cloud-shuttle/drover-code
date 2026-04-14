package config

import (
	"encoding/json"
	"testing"
)

func FuzzMergeInto(f *testing.F) {
	f.Add([]byte(`{}`), []byte(`{"model":"x"}`))
	f.Add([]byte(`{"model":"a","maxTokens":100}`), []byte(`{"model":"b"}`))

	f.Fuzz(func(t *testing.T, a, b []byte) {
		var base, layer Settings
		if err := json.Unmarshal(a, &base); err != nil {
			return
		}
		if err := json.Unmarshal(b, &layer); err != nil {
			return
		}
		mergeInto(&base, layer)
	})
}
