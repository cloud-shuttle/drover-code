package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudshuttle/drover-code/internal/config"
)

func runCloudMode(ctx context.Context, workDir string, settings config.Settings) {
	fmt.Println("☁️ Drover Cloud Mode")
	fmt.Printf("Packing workspace %s and sending to Drover Cloud API...\n", workDir)

	// Phase 1 stub: Define the API contract
	// 1. Tar the workspace
	// 2. POST /api/v1/jobs
	// 3. Connect to SSE stream GET /api/v1/jobs/:id/stream
	// 4. Download and extract workspace GET /api/v1/jobs/:id/workspace
	
	fmt.Println("Error: Drover Cloud API endpoint not yet defined.")
	os.Exit(1)
}
