package github

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var mentionRe = regexp.MustCompile(`(?i)@drover-code\s+(.+)`)

func ParseWebhook(eventType EventType, body []byte) (*ParsedEvent, error) {
	ev := &ParsedEvent{Type: eventType, Raw: body}

	switch eventType {
	case EventIssueComment:
		var payload IssueCommentEvent
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("parse issue_comment: %w", err)
		}
		ev.Trigger = extractFromIssueComment(payload)

	case EventPRReviewComment:
		var payload PRReviewCommentEvent
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("parse pr_review_comment: %w", err)
		}
		ev.Trigger = extractFromReviewComment(payload)
	}

	return ev, nil
}

func extractFromIssueComment(e IssueCommentEvent) *Trigger {
	if e.Action != "created" {
		return nil
	}

	request := extractMention(e.Comment.Body)
	if request == "" {
		return nil
	}

	owner, repo := splitFullName(e.Repository.FullName)
	isPR := e.PullRequest != nil || e.Issue.PullRequest()

	ctx := TriggerContext{
		Repo:        e.Repository,
		IssueNumber: e.Issue.Number,
		IssuTitle:   e.Issue.Title,
		IssueBody:   e.Issue.Body,
		CommentBody: e.Comment.Body,
	}

	if isPR && e.PullRequest != nil {
		ctx.PRNumber = e.PullRequest.Number
		ctx.PRHead = e.PullRequest.Head.Ref
		ctx.PRBase = e.PullRequest.Base.Ref
	} else if isPR {
		ctx.PRNumber = e.Issue.Number
	}

	return &Trigger{
		Request: request,
		Context: ctx,
		ReplyTarget: ReplyTarget{
			Kind:   ReplyIssueComment,
			Owner:  owner,
			Repo:   repo,
			Number: e.Issue.Number,
		},
	}
}

func extractFromReviewComment(e PRReviewCommentEvent) *Trigger {
	if e.Action != "created" {
		return nil
	}

	request := extractMention(e.Comment.Body)
	if request == "" {
		return nil
	}

	owner, repo := splitFullName(e.Repository.FullName)

	return &Trigger{
		Request: request,
		Context: TriggerContext{
			Repo:        e.Repository,
			PRNumber:    e.PullRequest.Number,
			PRHead:      e.PullRequest.Head.Ref,
			PRBase:      e.PullRequest.Base.Ref,
			IssuTitle:   e.PullRequest.Title,
			IssueBody:   e.PullRequest.Body,
			CommentBody: e.Comment.Body,
			DiffContext: e.Comment.DiffHunk,
			FilePath:    e.Comment.Path,
			DiffLine:    e.Comment.Line,
		},
		ReplyTarget: ReplyTarget{
			Kind:      ReplyReviewComment,
			Owner:     owner,
			Repo:      repo,
			Number:    e.PullRequest.Number,
			CommitSHA: e.Comment.CommitID,
			FilePath:  e.Comment.Path,
			Line:      e.Comment.Line,
		},
	}
}

func extractMention(s string) string {
	m := mentionRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	request := strings.TrimSpace(m[1])

	lines := strings.Split(s, "\n")
	var inRequest bool
	var parts []string
	for _, line := range lines {
		if mentionRe.MatchString(line) {
			inRequest = true
			after := mentionRe.FindStringSubmatch(line)[1]
			parts = append(parts, strings.TrimSpace(after))
			continue
		}
		if inRequest {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				break
			}
			if !strings.HasPrefix(trimmed, "@") {
				parts = append(parts, trimmed)
			}
		}
	}
	if len(parts) > 0 {
		request = strings.Join(parts, " ")
	}
	return request
}

func splitFullName(fullName string) (owner, repo string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return fullName, ""
}

func (i *Issue) PullRequest() bool {
	return strings.Contains(i.HTMLURL, "/pull/")
}

