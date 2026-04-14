package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	maxBodyBytes  = 25 * 1024 * 1024
	maxConcurrent = 5
	jobTimeout    = 10 * time.Minute
)

// WebhookRunner runs a job produced by a GitHub webhook delivery.
type WebhookRunner interface {
	Handle(ctx context.Context, trigger *Trigger) error
}

type Server struct {
	runner     WebhookRunner
	secret     string
	sem        chan struct{}
	mu         sync.Mutex
	activeJobs map[string]bool
}

func NewServer(runner WebhookRunner, secret string) *Server {
	return &Server{
		runner:     runner,
		secret:     secret,
		sem:        make(chan struct{}, maxConcurrent),
		activeJobs: make(map[string]bool),
	}
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.handleWebhook)
}

// HTTPServer returns an http.Server wired to the GitHub webhook handler and /health.
// Use ListenAndServe, or Serve on a net.Listener, and Shutdown for graceful stop.
func (s *Server) HTTPServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/webhooks/github", s.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
}

func (s *Server) ListenAndServe(addr string) error {
	srv := s.HTTPServer(addr)
	log.Printf("github webhook server listening on %s", addr)
	return srv.ListenAndServe()
}

// handleWebhook accepts GitHub webhook POSTs.
//
// Limits: if Content-Length is set and exceeds maxBodyBytes, responds 413 without reading the body.
// Otherwise at most maxBodyBytes are read (io.LimitReader); a longer payload is truncated and may
// fail JSON parse. Chunked or unknown-length requests are not rejected by Content-Length but are
// still capped by that read limit.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cl := r.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n > int64(maxBodyBytes) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	if s.secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if err := VerifySignature(body, sig, s.secret); err != nil {
			log.Printf("webhook signature verification failed: %v", err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	eventType := EventType(r.Header.Get("X-GitHub-Event"))
	deliveryID := r.Header.Get("X-GitHub-Delivery")

	parsed, err := ParseWebhook(eventType, body)
	if err != nil {
		log.Printf("webhook parse error [%s]: %v", deliveryID, err)
		http.Error(w, "parse error", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "delivery": deliveryID})

	if parsed.Trigger == nil {
		return
	}

	jobKey := fmt.Sprintf("%s/%s#%d",
		parsed.Trigger.ReplyTarget.Owner,
		parsed.Trigger.ReplyTarget.Repo,
		parsed.Trigger.ReplyTarget.Number,
	)

	s.mu.Lock()
	if s.activeJobs[jobKey] {
		s.mu.Unlock()
		log.Printf("webhook: job already active for %s, skipping delivery %s", jobKey, deliveryID)
		return
	}
	s.activeJobs[jobKey] = true
	s.mu.Unlock()

	trigger := parsed.Trigger
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.activeJobs, jobKey)
			s.mu.Unlock()
		}()

		s.sem <- struct{}{}
		defer func() { <-s.sem }()

		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()

		log.Printf("webhook: running job for %s (delivery %s): %q",
			jobKey, deliveryID, truncate(trigger.Request, 60))

		if err := s.runner.Handle(ctx, trigger); err != nil {
			log.Printf("webhook: job error for %s: %v", jobKey, err)
		} else {
			log.Printf("webhook: job complete for %s", jobKey)
		}
	}()
}

func (s *Server) Metrics() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"active_jobs":         len(s.activeJobs),
		"max_concurrent":      maxConcurrent,
		"semaphore_available": maxConcurrent - len(s.sem),
	}
}
