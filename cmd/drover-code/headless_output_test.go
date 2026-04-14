package main

import (
	"os"
	"testing"
)

func TestHeadlessUseJSONL_envOverrides(t *testing.T) {
	t.Setenv("DROVER_CODE_HEADLESS_PLAIN", "1")
	t.Setenv("DROVER_CODE_JSONL", "1")
	if headlessUseJSONL() {
		t.Fatal("HEADLESS_PLAIN=1 must win over JSONL=1")
	}

	t.Setenv("DROVER_CODE_HEADLESS_PLAIN", "")
	t.Setenv("DROVER_CODE_JSONL", "1")
	if !headlessUseJSONL() {
		t.Fatal("JSONL=1 must force JSONL")
	}
}

func TestHeadlessUseJSONL_stdoutStat(t *testing.T) {
	t.Setenv("DROVER_CODE_HEADLESS_PLAIN", "")
	t.Setenv("DROVER_CODE_JSONL", "")
	// os.Stdout in tests is usually not a character device → JSONL path.
	if !headlessUseJSONL() {
		// If the test runner attaches a TTY to stdout, plain is correct.
		stat, err := os.Stdout.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			t.Fatal("expected JSONL when stdout is not a TTY")
		}
	}
}
