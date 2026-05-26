package outcomesignal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

const insertSpanSQL = `
	INSERT INTO drover_trace.spans (
		trace_id, org_id, name, status, start_time, end_time, attributes
	) VALUES (?, ?, ?, ?, ?, ?, ?)
`

func writeSpan(dsn, orgID, traceID, agentSlug string, signals Signals, inputPrompt, outputText string, extra map[string]any) error {
	m := signals.AttributesMap()
	for k, v := range extra {
		m[k] = v
	}
	if inputPrompt != "" {
		m["user_message"] = inputPrompt
		m["completion"] = outputText
	}
	attrs, err := json.Marshal(m)
	if err != nil {
		return err
	}

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return fmt.Errorf("outcomesignal: open: %w", err)
	}
	defer db.Close()

	now := time.Now().UTC()
	status := "ok"
	if signals.CompileSuccess != nil && !*signals.CompileSuccess {
		status = "error"
	}
	name := "AgentExecution:" + agentSlug

	_, err = db.ExecContext(context.Background(), insertSpanSQL,
		traceID, orgID, name, status, now.Add(-time.Second), now, string(attrs),
	)
	if err != nil {
		return fmt.Errorf("outcomesignal: insert span: %w", err)
	}
	return nil
}
