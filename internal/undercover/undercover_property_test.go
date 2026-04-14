package undercover

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestProperty_IsInternalDomainMonotone(t *testing.T) {
	// Extending a URL that is already internal keeps it internal.
	err := quick.Check(func(prefix, suffix string) bool {
		if len(prefix) > 200 || len(suffix) > 200 {
			return true
		}
		u := strings.ToLower(prefix + "github.anthropic.com" + suffix)
		if !isInternalDomain(u) {
			return false
		}
		return isInternalDomain(u + suffix + "extra")
	}, &quick.Config{MaxCount: 120})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProperty_PublicGitHubHostsNotInternal(t *testing.T) {
	err := quick.Check(func(org, repo string) bool {
		if len(org) > 64 || len(repo) > 64 {
			return true
		}
		if org == "" || repo == "" {
			return true
		}
		u := "https://github.com/" + org + "/" + repo + ".git"
		return !isInternalDomain(strings.ToLower(u))
	}, &quick.Config{MaxCount: 120})
	if err != nil {
		t.Fatal(err)
	}
}
