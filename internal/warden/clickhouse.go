package warden

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	droverwarden "github.com/cloud-shuttle/drover-warden/warden"
)

// ClickHouseLogger writes Warden decisions to the central drover_decisions / warden_decisions table
// in ClickHouse so they can be correlated with Guard decisions in HyperDX/ClickStack.
type ClickHouseLogger struct {
	conn clickhouse.Conn
}

func NewClickHouseLogger(dsn string) (*ClickHouseLogger, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{dsn},
		Auth: clickhouse.Auth{
			Database: "default",
			Username: "default",
			Password: "",
		},
	})
	if err != nil {
		return nil, err
	}
	return &ClickHouseLogger{conn: conn}, nil
}

// LogDecision writes a Warden semantic decision to ClickHouse.
// It is best-effort (errors are logged but do not fail the tool call).
func (l *ClickHouseLogger) LogDecision(ctx context.Context, checkType string, req *droverwarden.GuardRequest, dec droverwarden.Decision) error {
	if l.conn == nil {
		return nil
	}

	query := `
		INSERT INTO warden_decisions (
			decision_id, trace_id, tenant_id, agent_id, job_id,
			check_type, tool_name, allowed, reason, result,
			resource_id, latency_ms, environment, beads_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	toolName := ""
	if req.ToolCall != nil {
		toolName = req.ToolCall.ToolName
	}

	// Try to pull job_id / agent_id from context or env (best effort)
	jobID := ""
	if req.Context != nil {
		if j, ok := req.Context["job_id"].(string); ok {
			jobID = j
		}
	}

	return l.conn.Exec(ctx, query,
		uuid.New(),
		"", // trace_id - can be enriched later
		req.TenantID,
		getStringFromContext(req, "agent_id"),
		jobID,
		checkType,
		toolName,
		dec.Allowed,
		dec.Result.Reason,
		dec.Result.Result,
		"", // resource_id (can be enhanced)
		dec.Result.LatencyMS,
		"local", // environment
		"",      // beads_version
	)
}

func getStringFromContext(req *droverwarden.GuardRequest, key string) string {
	if req.Context == nil {
		return ""
	}
	if v, ok := req.Context[key].(string); ok {
		return v
	}
	return ""
}

// Optional: close connection on shutdown
func (l *ClickHouseLogger) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}

// Global optional logger (initialized in main if DROVER_WARDEN_CLICKHOUSE_DSN is set)
var chLogger *ClickHouseLogger

// InitClickHouseLogger is called from cmd/drover-code/main.go or ukc-agent if the DSN env is present.
func InitClickHouseLogger(dsn string) error {
	if dsn == "" {
		return nil
	}
	var err error
	chLogger, err = NewClickHouseLogger(dsn)
	return err
}

// logToClickHouse is the internal emission point (best effort)
func logToClickHouse(ctx context.Context, checkType string, req *droverwarden.GuardRequest, dec droverwarden.Decision) {
	if chLogger == nil {
		return
	}
	// Fire and forget
	go func() {
		_ = chLogger.LogDecision(ctx, checkType, req, dec)
	}()
}