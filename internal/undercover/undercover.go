// Package undercover implements undercover mode.
package undercover

import (
	"bytes"
	"os/exec"
	"strings"
)

const SystemPromptFragment = `## UNDERCOVER MODE — CRITICAL

You are operating in a PUBLIC or OPEN-SOURCE repository.
Your commit messages, PR titles, PR bodies, and code comments
MUST NOT contain ANY Anthropic-internal information.

NEVER include:
- Internal model codenames (animal names: Capybara, Tengu, Kestrel, etc.)
- Unreleased model version numbers (e.g. opus-4-7, sonnet-4-8)
- Internal repository or project names
- Internal tooling references (Slack channels, short links, etc.)
- Product names or branding that identify you as an AI or automated assistant
- Co-Authored-By lines or AI attribution of any kind

Write commit messages and PR descriptions as a human developer would.
Do not blow your cover under any circumstances.`

type Status struct {
	Active bool
	Reason string
}

func Detect(repoPath string) Status {
	remote, err := gitRemoteURL(repoPath)
	if err != nil || remote == "" {
		return Status{false, "no git remote"}
	}

	remote = strings.ToLower(remote)

	if strings.Contains(remote, "github.com") {
		if isInternalDomain(remote) {
			return Status{false, "internal github domain"}
		}
		return Status{true, "public github remote: " + remote}
	}
	if strings.Contains(remote, "gitlab.com") || strings.Contains(remote, "bitbucket.org") {
		return Status{true, "public hosting remote: " + remote}
	}
	return Status{false, "unrecognised remote: " + remote}
}

func gitRemoteURL(repoPath string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoPath
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

func isInternalDomain(url string) bool {
	internal := []string{
		"github.anthropic.com",
		"github.internal.",
		"github.corp.",
		"ghe.",
	}
	for _, d := range internal {
		if strings.Contains(url, d) {
			return true
		}
	}
	return false
}

