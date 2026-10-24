package main

import (
	"os"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/pkg/sessionevent"
)

// headlessSession is set for the life of runHeadless so the event drains
// and the watchdog can append without threading a logger through every exit.
var headlessSession sessionLog

type sessionLog struct {
	path      string
	sessionID string
	issueID   string
	phase     string
	envelope  bool
}

func sessionLogFromEnv() sessionLog {
	sid := os.Getenv("DROVER_SESSION_ID")
	if sid == "" {
		sid = "local"
	}
	return sessionLog{
		path:      os.Getenv("DROVER_SESSION_EVENTS"),
		sessionID: sid,
		issueID:   os.Getenv("DROVER_ISSUE_ID"),
		phase:     os.Getenv("DROVER_PHASE"),
		envelope:  os.Getenv("DROVER_SESSION_EVENTS_ENVELOPE") != "0",
	}
}

func (l sessionLog) enabled() bool { return l.path != "" }

func (l sessionLog) append(typ string, data map[string]any) {
	if l.path == "" {
		return
	}
	ev := sessionevent.New(typ, l.sessionID, data)
	ev.IssueID = l.issueID
	ev.Phase = l.phase
	_ = sessionevent.Append(l.path, ev)
}

func (l sessionLog) begin() {
	if !l.enabled() || !l.envelope {
		return
	}
	l.append(sessionevent.TypeStarted, nil)
	if l.issueID != "" {
		l.append(sessionevent.TypeLinked, map[string]any{
			"issue_id": l.issueID,
			"phase":    l.phase,
		})
	}
	l.append(sessionevent.TypeStatus, map[string]any{"status": "running"})
}

func (l sessionLog) userMessage(text string) {
	if !l.enabled() {
		return
	}
	l.append(sessionevent.TypeMessage, map[string]any{
		"role": "user",
		"text": trimRunes(text, 500),
	})
}

func recordSessionObserve(ev agent.Event) {
	if !headlessSession.enabled() {
		return
	}
	switch e := ev.(type) {
	case agent.ToolStartEvent:
		headlessSession.append(sessionevent.TypeToolRequested, map[string]any{
			"name": e.Name,
			"id":   e.ID,
		})
		if artifactTool(e.Name) {
			headlessSession.append(sessionevent.TypeArtifactWritten, map[string]any{
				"path": e.InputSummary,
				"kind": e.Name,
			})
		}
	case agent.ToolDoneEvent:
		headlessSession.append(sessionevent.TypeToolFinished, map[string]any{
			"name":     e.Name,
			"id":       e.ID,
			"is_error": e.IsError,
		})
	case agent.PermissionRequestEvent:
		headlessSession.append(sessionevent.TypeApprovalRequested, map[string]any{
			"tool_name": e.ToolName,
		})
	case agent.PermissionBatchRequestEvent:
		headlessSession.append(sessionevent.TypeApprovalRequested, map[string]any{
			"count": len(e.Items),
		})
	}
}

func artifactTool(name string) bool {
	switch name {
	case "write", "write_file", "edit", "edit_file", "multi_edit", "apply_patch":
		return true
	default:
		return false
	}
}

func (l sessionLog) finish(status string) {
	l.append(sessionevent.TypeStatus, map[string]any{"status": status})
}

func exitHeadless(code int, status string) {
	headlessSession.finish(status)
	os.Exit(code)
}

func trimRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := range s {
		if i == n {
			return s[:k]
		}
		i++
	}
	return s
}
