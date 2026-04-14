package github

import (
	"encoding/json"
	"testing"
)

func FuzzParseWebhook(f *testing.F) {
	seedIssue := map[string]any{
		"action": "created",
		"comment": map[string]any{
			"body": "@drover-code hello",
		},
		"issue": map[string]any{
			"number":   float64(1),
			"title":    "t",
			"html_url": "https://github.com/o/r/issues/1",
		},
		"repository": map[string]any{"full_name": "o/r"},
	}
	rawIssue, _ := json.Marshal(seedIssue)
	f.Add(byte(0), rawIssue)

	seedReview := map[string]any{
		"action": "created",
		"comment": map[string]any{
			"body": "@drover-code review this",
		},
		"pull_request": map[string]any{
			"number": float64(2),
			"title":  "pr",
			"head":   map[string]any{"ref": "h"},
			"base":   map[string]any{"ref": "main"},
		},
		"repository": map[string]any{"full_name": "o/r"},
	}
	rawReview, _ := json.Marshal(seedReview)
	f.Add(byte(1), rawReview)

	f.Fuzz(func(t *testing.T, tag byte, body []byte) {
		var et EventType
		if tag%2 == 0 {
			et = EventIssueComment
		} else {
			et = EventPRReviewComment
		}
		_, _ = ParseWebhook(et, body)
	})
}

func FuzzExtractMention(f *testing.F) {
	f.Add("@drover-code one line")
	f.Add("@drover-code first\nsecond line\n")
	f.Fuzz(func(t *testing.T, s string) {
		_ = extractMention(s)
	})
}
