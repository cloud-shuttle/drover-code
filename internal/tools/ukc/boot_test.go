package ukc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const edgeNoService = `<!doctype html><html><head><title>Service not found</title></head><body>There is no service on this URL.</body></html>`

// bootServer serves GET /v1/instances/u from instanceJSON() and /health from health().
func bootServer(t *testing.T, instanceJSON func() string, health func() (int, string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/instances/u":
			if r.Header.Get("Authorization") != "Bearer api" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"status":"success","data":{"instances":[%s]}}`, instanceJSON())
		case r.URL.Path == "/health":
			if r.Header.Get("Authorization") != "Bearer agent" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			code, body := health()
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func bootCfg(srv *httptest.Server) Config {
	return Config{Token: "api", APIBaseURL: srv.URL, HTTPClient: srv.Client()}
}

func TestWaitForWorker_StoppedOnBootFailsFast(t *testing.T) {
	// Sydney 20 Nov 2026: started_at == stopped_at, stop_reason=4 (platform), no console.
	srv := bootServer(t,
		func() string {
			return `{"uuid":"u","state":"stopped","stop_reason":4,"image":"oci://syd0-plausible-gnu.unikraft.cloud/cloudshuttle/ukc-agent:latest"}`
		},
		func() (int, string) { return http.StatusNotFound, edgeNoService },
	)
	defer srv.Close()
	start := time.Now()
	err := WaitForWorker(context.Background(), bootCfg(srv), "u", srv.URL, "agent", BootWatch{MaxWait: 30 * time.Second, Poll: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("expected error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("did not fail fast: %s", time.Since(start))
	}
	for _, want := range []string{"stopped while booting", "stop_reason=4 (platform)", "ukc-agent:latest", "publish ukc-agent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestWaitForWorker_AppExitReportsExitCode(t *testing.T) {
	srv := bootServer(t,
		func() string { return `{"uuid":"u","state":"stopped","stop_reason":3,"stop_code":0,"exit_code":1}` },
		func() (int, string) { return http.StatusNotFound, edgeNoService },
	)
	defer srv.Close()
	err := WaitForWorker(context.Background(), bootCfg(srv), "u", srv.URL, "agent", BootWatch{MaxWait: 5 * time.Second, Poll: 10 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "exit_code=1") || !strings.Contains(err.Error(), "app exited during boot") {
		t.Fatalf("got %v", err)
	}
}

func TestWaitForWorker_RunningButNoRouteFails(t *testing.T) {
	srv := bootServer(t,
		func() string { return `{"uuid":"u","state":"running"}` },
		func() (int, string) { return http.StatusNotFound, edgeNoService },
	)
	defer srv.Close()
	err := WaitForWorker(context.Background(), bootCfg(srv), "u", srv.URL, "agent", BootWatch{MaxWait: 10 * time.Second, RouteGrace: 50 * time.Millisecond, Poll: 10 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "worker route missing") {
		t.Fatalf("got %v", err)
	}
}

func TestWaitForWorker_StartingNoServiceKeepsWaiting(t *testing.T) {
	// While the instance is still starting, "no service" is normal and must not fail early.
	srv := bootServer(t,
		func() string { return `{"uuid":"u","state":"starting"}` },
		func() (int, string) { return http.StatusNotFound, edgeNoService },
	)
	defer srv.Close()
	err := WaitForWorker(context.Background(), bootCfg(srv), "u", srv.URL, "agent", BootWatch{MaxWait: 200 * time.Millisecond, RouteGrace: 10 * time.Millisecond, Poll: 10 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timeout after") || !strings.Contains(err.Error(), "edge has no service") {
		t.Fatalf("got %v", err)
	}
}

func TestWaitForWorker_HealthyReportsPhases(t *testing.T) {
	var polls atomic.Int32
	srv := bootServer(t,
		func() string { return `{"uuid":"u","state":"running"}` },
		func() (int, string) {
			if polls.Add(1) < 3 {
				return http.StatusNotFound, edgeNoService
			}
			return http.StatusOK, "ok"
		},
	)
	defer srv.Close()
	var phases []string
	err := WaitForWorker(context.Background(), bootCfg(srv), "u", srv.URL, "agent", BootWatch{
		MaxWait: 5 * time.Second, Poll: 10 * time.Millisecond,
		OnPhase: func(p string, _ time.Duration) { phases = append(phases, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(phases, ",") != "running,healthy" {
		t.Fatalf("phases %v", phases)
	}
}

func TestDescribeStop(t *testing.T) {
	r, c := 7, 0
	got := DescribeStop(Instance{State: "stopped", StopReason: &r, StopCode: &c})
	if got != "state=stopped stop_reason=7 (kernel exited, app exited, platform) stop_code=0" {
		t.Fatal(got)
	}
}
