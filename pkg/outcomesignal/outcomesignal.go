// Package outcomesignal writes Drover Learner outcome keys on drover_trace.spans.
// See drover-learner/docs/reference/outcome-signal-contract.md.
package outcomesignal

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Canonical attribute keys (see drover-learner/docs/reference/outcome-signal-contract.md).
const (
	AttrCompileSuccess = "compile_success"
	AttrTestsPassed    = "tests_passed"
	AttrGitMergeMerged = "git_merge_merged"
	AttrAgentJobID     = "agent-job-id"
)

// Signals holds optional engineering feedback for Learner classification.
type Signals struct {
	CompileSuccess *bool
	TestsPassed    *bool
	GitMergeMerged *bool
}

// FromRunError maps a BYOC agent run result to outcome signals.
func FromRunError(runErr error) Signals {
	if runErr == nil {
		t := true
		return Signals{CompileSuccess: &t, TestsPassed: &t}
	}
	f := false
	return Signals{CompileSuccess: &f, TestsPassed: &f}
}

// FromHostedJob maps a terminal Cloud agent job status and merge outcome to learner signals.
// terminalStatus is the persisted job status (e.g. succeeded, failed, merge_conflict).
// mergeOutcome is the VCS integration outcome (e.g. merged, no_changes, merge_conflict).
func FromHostedJob(terminalStatus, mergeOutcome string) Signals {
	okRun := terminalStatus == "succeeded" || terminalStatus == "merge_conflict"
	f := false
	t := true
	if !okRun {
		return Signals{CompileSuccess: &f, TestsPassed: &f, GitMergeMerged: &f}
	}
	merged := mergeOutcome == "merged"
	return Signals{CompileSuccess: &t, TestsPassed: &t, GitMergeMerged: &merged}
}

// AttributesMap returns span attributes for ClickHouse JSON column.
func (s Signals) AttributesMap() map[string]any {
	m := make(map[string]any)
	if s.CompileSuccess != nil {
		m[AttrCompileSuccess] = *s.CompileSuccess
	}
	if s.TestsPassed != nil {
		m[AttrTestsPassed] = *s.TestsPassed
	}
	if s.GitMergeMerged != nil {
		m[AttrGitMergeMerged] = *s.GitMergeMerged
	}
	return m
}

// AttributesJSON returns a JSON object for drover_trace.spans.attributes.
func (s Signals) AttributesJSON() (string, error) {
	b, err := json.Marshal(s.AttributesMap())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WriteBYOCSpanIfConfigured inserts a completed AgentExecution span when DROVER_TRACE_DB_URL is set.
func WriteBYOCSpanIfConfigured(traceID string, signals Signals, inputPrompt, outputText string) error {
	dsn := strings.TrimSpace(os.Getenv("DROVER_TRACE_DB_URL"))
	if dsn == "" {
		return nil
	}
	orgID := envOr("DROVER_LEARNER_ORG_ID", envOr("DROVER_ORG_ID", "default"))
	agentSlug := envOr("DROVER_AGENT_SLUG", envOr("DROVER_AGENT_ID", "default"))
	if traceID == "" {
		traceID = fmt.Sprintf("byoc-%d", time.Now().UnixNano())
	}
	return writeSpan(dsn, orgID, traceID, agentSlug, signals, inputPrompt, outputText, nil)
}

// WriteHostedJobOutcomeSpanIfConfigured records terminal job outcome for Learner crawl.
// traceID should match the hosted job id (aj-…) so spans group with Gateway-audited traffic.
func WriteHostedJobOutcomeSpanIfConfigured(traceID, orgID, agentSlug, terminalStatus, mergeOutcome, prompt string) error {
	dsn := strings.TrimSpace(os.Getenv("DROVER_TRACE_DB_URL"))
	if dsn == "" {
		return nil
	}
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil
	}
	if strings.TrimSpace(orgID) == "" {
		orgID = "default"
	}
	if strings.TrimSpace(agentSlug) == "" {
		agentSlug = "hosted-agent"
	}
	extra := map[string]any{AttrAgentJobID: traceID}
	return writeSpan(dsn, orgID, traceID, agentSlug, FromHostedJob(terminalStatus, mergeOutcome), prompt, "", extra)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
