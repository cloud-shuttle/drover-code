package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestVerifySignature(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"hook":true}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if err := VerifySignature(body, sig, secret); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(body, "sha256=deadbeef", secret); err == nil {
		t.Fatal("expected mismatch")
	}
	if err := VerifySignature(body, "", secret); err == nil {
		t.Fatal("expected missing header error")
	}
}

func TestParseWebhook_IssueComment_NoMention(t *testing.T) {
	payload := map[string]any{
		"action": "created",
		"comment": map[string]any{
			"body": "no bot mention here",
		},
		"issue": map[string]any{
			"number":   float64(7),
			"title":    "Q",
			"html_url": "https://github.com/o/r/issues/7",
		},
		"repository": map[string]any{
			"full_name": "o/r",
		},
	}
	raw, _ := json.Marshal(payload)
	parsed, err := ParseWebhook(EventIssueComment, raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Trigger != nil {
		t.Fatalf("expected no trigger, got %+v", parsed.Trigger)
	}
}

func TestParseWebhook_UnsupportedEventType(t *testing.T) {
	parsed, err := ParseWebhook(EventType("ping"), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Type != "ping" || parsed.Trigger != nil {
		t.Fatalf("got type=%q trigger=%v", parsed.Type, parsed.Trigger)
	}
}

func TestParseWebhook_IssueComment_Mention(t *testing.T) {
	payload := map[string]any{
		"action": "created",
		"comment": map[string]any{
			"body": "@drover-code please explain this issue",
		},
		"issue": map[string]any{
			"number":   float64(42),
			"title":    "Bug",
			"html_url": "https://github.com/o/r/issues/42",
		},
		"repository": map[string]any{
			"full_name": "o/r",
		},
	}
	raw, _ := json.Marshal(payload)

	parsed, err := ParseWebhook(EventIssueComment, raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Trigger == nil {
		t.Fatal("expected trigger")
	}
	if parsed.Trigger.Request != "please explain this issue" {
		t.Fatalf("request %q", parsed.Trigger.Request)
	}
	if parsed.Trigger.ReplyTarget.Owner != "o" || parsed.Trigger.ReplyTarget.Repo != "r" {
		t.Fatalf("target %+v", parsed.Trigger.ReplyTarget)
	}
}

func TestServer_Webhook_AcceptedWithoutRunner(t *testing.T) {
	var calls atomic.Int32
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		calls.Add(1)
		return nil
	})

	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload := minimalIssueMentionPayload()
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", string(EventIssueComment))
	req.Header.Set("X-GitHub-Delivery", "del-1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", resp.StatusCode)
	}

	deadline := time.After(2 * time.Second)
	for calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("runner not invoked")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestServer_InvalidSignature(t *testing.T) {
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error { return nil })
	srv := NewServer(stub, "secret")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Hub-Signature-256", "sha256=abcd")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestServer_WrongHTTPMethod(t *testing.T) {
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error { return nil })
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestServer_UnsupportedEventAcceptedWithoutRunner(t *testing.T) {
	var calls atomic.Int32
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		calls.Add(1)
		return nil
	})
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-GitHub-Delivery", "del-ping")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", resp.StatusCode)
	}
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("runner should not run when trigger is nil")
	}
}

func TestServer_SkipsSecondDeliveryWhileJobActive(t *testing.T) {
	var calls atomic.Int32
	unblock := make(chan struct{})
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		calls.Add(1)
		<-unblock
		return nil
	})
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body, _ := json.Marshal(minimalIssueMentionPayload())
	post := func(delivery string) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", string(EventIssueComment))
		req.Header.Set("X-GitHub-Delivery", delivery)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if post("del-a") != http.StatusAccepted {
		t.Fatal("first delivery")
	}
	deadline := time.After(2 * time.Second)
	for calls.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("runner not started")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if post("del-b") != http.StatusAccepted {
		t.Fatal("second delivery should still return 202")
	}
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("expected 1 runner invocation, got %d", calls.Load())
	}
	close(unblock)
}

func TestServer_MetricsWhileJobRunning(t *testing.T) {
	unblock := make(chan struct{})
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		<-unblock
		return nil
	})
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	httpDone := make(chan struct{})
	go func() {
		defer close(httpDone)
		body, _ := json.Marshal(issueMentionPayloadForIssue(99))
		req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
		if err != nil {
			t.Error(err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", string(EventIssueComment))
		req.Header.Set("X-GitHub-Delivery", "del-metrics")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("status %d", resp.StatusCode)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	m := srv.Metrics()
	if n, ok := m["active_jobs"].(int); !ok || n != 1 {
		t.Fatalf("active_jobs want1, metrics=%v", m)
	}
	if avail, ok := m["semaphore_available"].(int); !ok || avail != maxConcurrent-1 {
		t.Fatalf("semaphore_available want %d, got %v", maxConcurrent-1, m["semaphore_available"])
	}

	close(unblock)
	<-httpDone
	time.Sleep(50 * time.Millisecond)
	if n, _ := srv.Metrics()["active_jobs"].(int); n != 0 {
		t.Fatalf("after job active_jobs=%d", n)
	}
}

// TestServer_OversizedBody_StillAcceptsWhenValidJSONWithinLimit documents LimitReader(maxBodyBytes):
// the first maxBodyBytes must be valid JSON; extra bytes on the wire are ignored.
// Uses a streaming body (no declared Content-Length) so the 413 fast-path does not apply.
func TestServer_OversizedBody_StillAcceptsWhenValidJSONWithinLimit(t *testing.T) {
	var calls atomic.Int32
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		calls.Add(1)
		return nil
	})
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	core, err := json.Marshal(issueMentionPayloadForIssue(777))
	if err != nil {
		t.Fatal(err)
	}
	padding := maxBodyBytes - len(core) + 128*1024
	if padding < 0 {
		t.Fatalf("core len %d exceeds maxBodyBytes", len(core))
	}
	pad := bytes.Repeat([]byte{' '}, padding)

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		if _, err := pw.Write(core); err != nil {
			return
		}
		_, _ = pw.Write(pad)
	}()

	req, err := http.NewRequest(http.MethodPost, ts.URL, pr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", string(EventIssueComment))
	req.Header.Set("X-GitHub-Delivery", "del-oversize-ok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", resp.StatusCode)
	}
	deadline := time.After(2 * time.Second)
	for calls.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("runner not invoked")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestServer_RejectedWhenContentLengthExceedsMax returns 413 before reading the full body.
func TestServer_RejectedWhenContentLengthExceedsMax(t *testing.T) {
	var calls atomic.Int32
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		calls.Add(1)
		return nil
	})
	srv := NewServer(stub, "")

	body, err := json.Marshal(minimalIssueMentionPayload())
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", string(EventIssueComment))
	req.Header.Set("X-GitHub-Delivery", "del-413")
	req.ContentLength = int64(maxBodyBytes) + 1
	req.Header.Set("Content-Length", fmt.Sprintf("%d", maxBodyBytes+1))

	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d want 413", rec.Code)
	}
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("runner should not run, calls=%d", calls.Load())
	}
}

// TestServer_LargeBody_ParseErrorWhenTruncatedMidJSON: truncation can land inside a string, yielding400.
// Streaming body avoids Content-Length > cap (which would yield 413 before LimitReader runs).
func TestServer_LargeBody_ParseErrorWhenTruncatedMidJSON(t *testing.T) {
	var calls atomic.Int32
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		calls.Add(1)
		return nil
	})
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	prefix := []byte(`{"action":"created","pad":"`)
	middle := bytes.Repeat([]byte{'z'}, maxBodyBytes)
	suffix := []byte(`"}`)

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		if _, err := pw.Write(prefix); err != nil {
			return
		}
		if _, err := pw.Write(middle); err != nil {
			return
		}
		_, _ = pw.Write(suffix)
	}()

	req, err := http.NewRequest(http.MethodPost, ts.URL, pr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", string(EventIssueComment))
	req.Header.Set("X-GitHub-Delivery", "del-truncate-bad")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d want 400", resp.StatusCode)
	}
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("runner should not run, calls=%d", calls.Load())
	}
}

func TestServer_ConcurrentJobsRespectSemaphore(t *testing.T) {
	var mu sync.Mutex
	cur := 0
	peak := 0
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			cur--
			mu.Unlock()
		}()
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
		}
		return nil
	})
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var wg sync.WaitGroup
	for i := 1; i <= 6; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, _ := json.Marshal(issueMentionPayloadForIssue(i))
			req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-GitHub-Event", string(EventIssueComment))
			req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("del-%d", i))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				t.Errorf("issue %d: status %d", i, resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	// Handlers acquire the semaphore before Run; allow time to reach peak concurrency.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	gotPeak := peak
	mu.Unlock()
	if gotPeak > maxConcurrent {
		t.Fatalf("peak concurrent Handle=%d want <= %d", gotPeak, maxConcurrent)
	}
	if gotPeak < maxConcurrent {
		t.Fatalf("peak concurrent Handle=%d want %d (6 jobs, 5 slots)", gotPeak, maxConcurrent)
	}
}

func TestServer_SecondDeliveryRunsAfterRunnerError(t *testing.T) {
	var calls atomic.Int32
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error {
		calls.Add(1)
		return fmt.Errorf("runner failed")
	})
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body, _ := json.Marshal(minimalIssueMentionPayload())
	post := func(delivery string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", string(EventIssueComment))
		req.Header.Set("X-GitHub-Delivery", delivery)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}

	post("del-1")
	deadline := time.After(3 * time.Second)
	for calls.Load() < 1 {
		select {
		case <-deadline:
			t.Fatalf("first run: calls=%d", calls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(5 * time.Millisecond) {
		if n, _ := srv.Metrics()["active_jobs"].(int); n == 0 {
			break
		}
	}
	if n, ok := srv.Metrics()["active_jobs"].(int); !ok || n != 0 {
		t.Fatalf("active_jobs want 0, metrics=%v", srv.Metrics())
	}

	post("del-2")
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("second run: calls=%d", calls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestServer_ParseErrorBadBody(t *testing.T) {
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error { return nil })
	srv := NewServer(stub, "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", string(EventIssueComment))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHTTPServer_ServeAndShutdown(t *testing.T) {
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error { return nil })
	s := NewServer(stub, "")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := s.HTTPServer("")
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	url := "http://" + ln.Addr().String()
	resp, err := http.Get(url + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != http.ErrServerClosed {
		t.Fatalf("Serve: %v", err)
	}

	_, err = http.Get(url + "/health")
	if err == nil {
		t.Fatal("expected error connecting after Shutdown")
	}
}

func TestServer_Health(t *testing.T) {
	stub := webhookFuncRunner(func(ctx context.Context, tr *Trigger) error { return nil })
	srv := NewServer(stub, "")
	mux := http.NewServeMux()
	mux.Handle("/webhooks/github", srv.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

type webhookFuncRunner func(ctx context.Context, tr *Trigger) error

func (f webhookFuncRunner) Handle(ctx context.Context, tr *Trigger) error {
	return f(ctx, tr)
}

func minimalIssueMentionPayload() map[string]any {
	return issueMentionPayloadForIssue(1)
}

func issueMentionPayloadForIssue(issueNum int) map[string]any {
	return map[string]any{
		"action": "created",
		"comment": map[string]any{
			"body": "@drover-code run tests",
		},
		"issue": map[string]any{
			"number":   float64(issueNum),
			"title":    "T",
			"html_url": fmt.Sprintf("https://github.com/acme/shop/issues/%d", issueNum),
		},
		"repository": map[string]any{
			"full_name": "acme/shop",
		},
	}
}
