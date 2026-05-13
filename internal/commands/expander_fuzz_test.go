package commands

import (
	"context"
	"testing"
)

func FuzzTemplateExpander(f *testing.F) {
	// Add seed corpus covering the different syntaxes
	f.Add("hello $1", "arg1")
	f.Add("bulk: $ARGUMENTS", "arg1 arg2")
	f.Add("run {test|default}", "")
	f.Add("include @file.txt", "")
	f.Add("shell !`echo test`", "")
	f.Add("mixed $1 $ARGUMENTS {var|val} @file.txt !`pwd`", "foo")
	f.Add("malformed {var|", "arg")
	f.Add("malformed !`echo", "arg")

	f.Fuzz(func(t *testing.T, template, arg string) {
		dir := t.TempDir()
		expander := NewTemplateExpander(dir)
		ctx := context.Background()

		// The goal of fuzzing here is to ensure the regexes and string manipulations
		// do not panic, hang indefinitely, or crash on malformed inputs.
		// We ignore the actual output string and error.
		_, _ = expander.Expand(ctx, template, []string{arg})
	})
}
