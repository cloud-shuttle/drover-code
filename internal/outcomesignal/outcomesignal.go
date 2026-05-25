// Package outcomesignal writes Drover Learner outcome keys for BYOC agent runs.
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
)

// Signals holds optional engineering feedback for Learner classification.
type Signals struct {
	CompileSuccess *bool
	TestsPassed    *bool
	GitMergeMerged *bool
}

// FromRunError maps a BYOC agent run result to outcome signals.
// Success sets compile and tests true; merge is left unset unless the caller sets it.
func FromRunError(runErr error) Signals {
	if runErr == nil {
		t := true
		return Signals{CompileSuccess: &t, TestsPassed: &t}
	}
	f := false
	return Signals{CompileSuccess: &f, TestsPassed: &f}
}

// AttributesJSON returns a JSON object for drover_trace.spans.attributes.
func (s Signals) AttributesJSON() (string, error) {
	m := make(map[string]bool)
	if s.CompileSuccess != nil {
		m[AttrCompileSuccess] = *s.CompileSuccess
	}
	if s.TestsPassed != nil {
		m[AttrTestsPassed] = *s.TestsPassed
	}
	if s.GitMergeMerged != nil {
		m[AttrGitMergeMerged] = *s.GitMergeMerged
	}
	b, err := json.Marshal(m)
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
	return writeSpan(dsn, orgID, traceID, agentSlug, signals, inputPrompt, outputText)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
