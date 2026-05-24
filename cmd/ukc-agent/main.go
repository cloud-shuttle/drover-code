// Command ukc-agent is the in-VM HTTP agent for Unikraft Cloud instances used with drover-code UKC tools.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cloudshuttle/drover-code/internal/telemetry"
	"github.com/cloudshuttle/drover-code/internal/warden"
)

const (
	defaultListen    = ":8080"
	healthPath       = "/health"
	workspacePath    = "/workspace"
	execPath         = "/exec"
	execStreamPath   = "/exec/" // + jobID + "/stream"
	headerAuth       = "Authorization"
	authBearerPrefix = "Bearer "
	headerBearerLen  = len(authBearerPrefix)
)

func main() {
	// Initialize Warden for semantic safety on hosted UKC workers (Option B).
	_ = warden.Init()

	// Optional: send Warden decisions to ClickHouse (same ClickStack instance used by Guard)
	if dsn := os.Getenv("DROVER_WARDEN_CLICKHOUSE_DSN"); dsn != "" {
		if err := warden.InitClickHouseLogger(dsn); err != nil {
			log.Printf("warning: failed to init Warden ClickHouse logger: %v", err)
		}
	}

	// Real OTEL exporter config for Warden semantic logs (OTLP → collector → guard_events).
	// Mirrors the setup in drover-code main; enables unified view in HyperDX governance dashboard.
	otelShutdown := telemetry.SetupOTELLogger(context.Background())
	defer func() { _ = otelShutdown(context.Background()) }()

	token := strings.TrimSpace(os.Getenv("AGENT_TOKEN"))
	if token == "" {
		log.Fatal("ukc-agent: AGENT_TOKEN is required")
	}

	addr := strings.TrimSpace(os.Getenv("AGENT_ADDR"))
	if addr == "" {
		if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
			addr = ":" + p
		} else {
			addr = defaultListen
		}
	}

	jobs := newJobRunner(token)

	mux := http.NewServeMux()
	mux.HandleFunc(healthPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkBearer(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc(workspacePath, func(w http.ResponseWriter, r *http.Request) {
		if !checkBearer(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			handleUploadWorkspace(w, r)
			return
		} else if r.Method == http.MethodGet {
			handleDownloadWorkspace(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc(execPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkBearer(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		body.Command = strings.TrimSpace(body.Command)
		if body.Command == "" {
			http.Error(w, "command required", http.StatusBadRequest)
			return
		}
		// Run in background context so the job survives the HTTP request
		id := jobs.start(context.Background(), body.Command)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": id})
	})
	mux.HandleFunc(execStreamPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkBearer(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, execPath)
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) < 2 || parts[1] != "stream" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		jobID := parts[0]
		if err := jobs.streamSSE(w, r, jobID); err != nil {
			// streamSSE writes headers; log only
			log.Printf("stream: %v", err)
		}
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
	}

	go func() {
		log.Printf("ukc-agent listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	shCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
}

func checkBearer(r *http.Request, want string) bool {
	h := r.Header.Get(headerAuth)
	if len(h) < headerBearerLen || !strings.EqualFold(h[:headerBearerLen], authBearerPrefix) {
		return false
	}
	got := strings.TrimSpace(h[headerBearerLen:])
	if got == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
