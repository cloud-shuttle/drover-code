package warden

// GuardRequest is the contract between drover-code and a bead file.
// drover-warden authors policies.jsonl. This module evaluates that file
// and does not import the private module.
type GuardRequest struct {
	RequestID string
	TenantID  string
	Input     string
	Output    any
	ToolCall  *ToolCall
	Context   map[string]any
	Metadata  map[string]string
}

// ToolCall is one tool invocation the action beads inspect.
type ToolCall struct {
	ToolName string
	Args     map[string]any
}

// GuardResult is the recorded outcome of one bead check.
type GuardResult struct {
	BeadID    string
	Result    string
	Reason    string
	LatencyMS int64
}

// Decision is what callers branch on. Allowed is false when a bead denies.
type Decision struct {
	Allowed bool
	Result  GuardResult
}
