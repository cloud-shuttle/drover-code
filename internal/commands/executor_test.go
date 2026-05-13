package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/pkg/guardclient"
)

func TestExecutor_E2E(t *testing.T) {
	// Setup mock Drover Guard server
	var requestedRiskTier int
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/evaluate" {
			t.Errorf("expected path /v1/evaluate, got %s", r.URL.Path)
		}
		
		var req guardclient.EvaluateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		
		requestedRiskTier = req.RiskTier

		resp := guardclient.EvaluateResponse{
			Allowed: true,
			Reason:  "mock approval",
		}
		// Deny if risk tier > 2
		if req.RiskTier > 2 {
			resp.Allowed = false
			resp.Reason = "risk too high"
			resp.HITLRequired = true
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	workDir := t.TempDir()

	// Scaffold a markdown command
	cmdDir := filepath.Join(workDir, ".drover", "commands")
	os.MkdirAll(cmdDir, 0755)

	safeCmd := `---
name: safe
description: safe cmd
risk_tier: 1
---
say $1`
	os.WriteFile(filepath.Join(cmdDir, "safe.md"), []byte(safeCmd), 0644)

	riskyCmd := `---
name: risky
description: risky cmd
risk_tier: 5
---
do dangerous thing`
	os.WriteFile(filepath.Join(cmdDir, "risky.md"), []byte(riskyCmd), 0644)

	// Setup client and executor
	os.Setenv("DROVER_GUARD_URL", mockServer.URL)
	defer os.Unsetenv("DROVER_GUARD_URL")
	
	guardClient := guardclient.NewClient(mockServer.URL, "mock-token")
	loader := NewLoader(workDir)
	_ = loader.LoadAll(config.Settings{})
	registry := loader.GetRegistry()
	
	expander := NewTemplateExpander(workDir)
	executor := NewExecutor(registry, expander, guardClient)

	ctx := context.Background()

	// Test 1: Safe command execution
	expanded, def, err := executor.EvaluateAndExpand(ctx, "safe", []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error on safe command: %v", err)
	}
	if expanded != "say hello" {
		t.Errorf("got %q, want 'say hello'", expanded)
	}
	if def.Name != "safe" {
		t.Errorf("got def name %q, want 'safe'", def.Name)
	}
	if requestedRiskTier != 1 {
		t.Errorf("got risk tier %d, want 1", requestedRiskTier)
	}

	// Test 2: Risky command execution
	_, _, err = executor.EvaluateAndExpand(ctx, "risky", []string{})
	if err == nil {
		t.Fatal("expected error on risky command but got none")
	}
	if err.Error() != "command blocked by Drover Guard: risk too high" {
		t.Errorf("got error %q, want Guard denial", err.Error())
	}
	if requestedRiskTier != 5 {
		t.Errorf("got risk tier %d, want 5", requestedRiskTier)
	}

	// Test 3: Unknown command
	_, _, err = executor.EvaluateAndExpand(ctx, "unknown", []string{})
	if err == nil {
		t.Fatal("expected error on unknown command")
	}
	if err.Error() != "command unknown not found" {
		t.Errorf("got error %q, want 'command unknown not found'", err.Error())
	}
}
