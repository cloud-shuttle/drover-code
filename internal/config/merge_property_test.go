package config

import (
	"testing"
	"testing/quick"
)

func TestMergeInto_quick_laterModelWins(t *testing.T) {
	err := quick.Check(func(m1, m2 string) bool {
		var dst Settings
		mergeInto(&dst, Settings{Model: m1})
		mergeInto(&dst, Settings{Model: m2})
		if m2 != "" {
			return dst.Model == m2
		}
		return dst.Model == m1
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatal(err)
	}
}
