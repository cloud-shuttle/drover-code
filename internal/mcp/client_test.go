package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClient_Lifecycle(t *testing.T) {
	dir, err := os.MkdirTemp("", "mcp-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	script := filepath.Join(dir, "mock-mcp.sh")
	content := `#!/bin/sh
# Read line by line and just echo an empty result for whatever ID comes in
while read line; do
  # extract ID using sed (simplistic)
  id=$(echo "$line" | grep -o '"id":[0-9]*' | cut -d':' -f2)
  if [ ! -z "$id" ]; then
    echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{}}"
  fi
done
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	c := NewClient([]string{script})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("failed to start client: %v", err)
	}

	var res map[string]any
	if err := c.Call(ctx, "test/method", nil, &res); err != nil {
		t.Fatalf("failed to call method: %v", err)
	}

	if err := c.Notify(ctx, "test/notify", nil); err != nil {
		t.Fatalf("failed to notify: %v", err)
	}

	if err := c.Stop(); err != nil {
		t.Fatalf("failed to stop: %v", err)
	}
}
