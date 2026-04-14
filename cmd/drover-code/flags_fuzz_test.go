package main

import (
	"bytes"
	"strings"
	"testing"
)

// argvFuzzMaxArgs limits argv length so fuzzing stays fast and bounded.
const argvFuzzMaxArgs = 128

// argvFuzzMaxArgLen limits each argument's rune/byte length.
const argvFuzzMaxArgLen = 2048

// argvFuzzMaxPayload caps total fuzz input bytes (flag parsing does not need more).
const argvFuzzMaxPayload = 8 << 10

// encodeArgvForFuzz packs argv for the fuzz corpus (NUL-separated tokens).
func encodeArgvForFuzz(argv []string) []byte {
	if len(argv) == 0 {
		return nil
	}
	return []byte(strings.Join(argv, "\x00"))
}

// decodeArgvFromFuzz unpacks fuzz bytes into argv. Empty input is nil argv.
func decodeArgvFromFuzz(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(string(data), "\x00")
}

func FuzzParseCLIFlags(f *testing.F) {
	seeds := [][]string{
		nil,
		{"--headless"},
		{"--prompt", "hello"},
		{"--prompt="},
		{"-p=", "orphan"},
		{"--prompt-file", "/tmp/x"},
		{"--prompt-file="},
		{"--unknown", "--headless", "-p", "x"},
		{"--prompt", "a", "--prompt-file", "b"},
		{"--prompt"},
		{"--prompt-file"},
	}
	for _, s := range seeds {
		f.Add(encodeArgvForFuzz(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > argvFuzzMaxPayload {
			return
		}
		// Avoid strings.Split allocating O(n NULs) slices before the argc check.
		if bytes.Count(data, []byte{0})+1 > argvFuzzMaxArgs {
			return
		}
		argv := decodeArgvFromFuzz(data)
		if len(argv) > argvFuzzMaxArgs {
			return
		}
		for _, a := range argv {
			if len(a) > argvFuzzMaxArgLen {
				return
			}
		}

		g1, err1 := parseCLIFlags(argv)
		g2, err2 := parseCLIFlags(argv)

		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("non-deterministic ok/err: err1=%v err2=%v argv=%q", err1, err2, argv)
		}
		if err1 != nil {
			if err1.Error() != err2.Error() {
				t.Fatalf("non-deterministic error text: %q vs %q argv=%q", err1, err2, argv)
			}
			return
		}
		if g1 != g2 {
			t.Fatalf("non-deterministic flags: %+v vs %+v argv=%q", g1, g2, argv)
		}
	})
}
