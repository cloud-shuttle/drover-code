//go:build darwin

package shell

import (
	"strings"
	"testing"
)

func TestSeatbeltProfileQuotesPaths(t *testing.T) {
	got := seatbeltProfile(`/tmp/a"b`, `/tmp/c\d`)
	if !strings.Contains(got, `(subpath "/tmp/a\"b")`) {
		t.Fatalf("quote was not escaped:\n%s", got)
	}
	if !strings.Contains(got, `(subpath "/tmp/c\\d")`) {
		t.Fatalf("backslash was not escaped:\n%s", got)
	}
	if !strings.Contains(got, "(deny network*)") {
		t.Fatalf("network was not denied:\n%s", got)
	}
}
