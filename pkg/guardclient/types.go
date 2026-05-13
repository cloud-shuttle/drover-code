package guardclient

// EvaluateRequest represents the payload sent to Drover Guard for evaluation.
type EvaluateRequest struct {
	TenantID     string                 `json:"tenant_id"`
	AgentID      string                 `json:"agent_id"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Permission   string                 `json:"permission"`
	RiskTier     int                    `json:"risk_tier"`
	Payload      map[string]interface{} `json:"payload,omitempty"`
}

// EvaluateResponse is the decision returned by Drover Guard.
type EvaluateResponse struct {
	Allowed      bool   `json:"allowed"`
	HITLRequired bool   `json:"hitl_required"`
	Reason       string `json:"reason"`
	PolicyID     string `json:"policy_id"`
	TraceID      string `json:"trace_id"`
}
