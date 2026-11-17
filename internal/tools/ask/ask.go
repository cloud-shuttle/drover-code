// Package ask is the local tool that stops and asks the person one question.
package ask

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const (
	maxQuestion = 2_000
	maxAnswer   = 8_000
)

// User asks one question and returns the typed answer.
// Ask is wired by the TUI. A nil Ask means this session cannot ask.
type User struct {
	Ask func(ctx context.Context, question string) (string, error)
}

func (t *User) Name() string { return "ask_user" }

func (t *User) Description() string {
	return "Ask the person one question and wait for their typed answer. " +
		"Use this when you need a decision you cannot infer. " +
		"One question at a time. The TUI shows the question. Local TUI only."
}

func (t *User) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("question", toolutil.NewSchema("string").Desc("The one question to ask")).
		Required("question").
		Build()
}

func (t *User) NeedsPermission(json.RawMessage) bool { return false }

func (t *User) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t == nil || t.Ask == nil {
		return "", fmt.Errorf("ask_user: not available in this session")
	}
	var in struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("ask_user: bad input: %w", err)
	}
	question := strings.TrimSpace(in.Question)
	if question == "" {
		return "", fmt.Errorf("ask_user: question is empty")
	}
	if utf8.RuneCountInString(question) > maxQuestion {
		return "", fmt.Errorf("ask_user: question is longer than %d characters", maxQuestion)
	}
	answer, err := t.Ask(ctx, question)
	if err != nil {
		return "", fmt.Errorf("ask_user: %w", err)
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("ask_user: empty answer")
	}
	return clip(answer, maxAnswer), nil
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := range s {
		if i == n-1 {
			return s[:k] + "…"
		}
		i++
	}
	return s
}
