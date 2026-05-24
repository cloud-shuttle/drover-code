package ukc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRegisterUnregisterActiveJob(t *testing.T) {
	dir := t.TempDir()
	oldHome := os.Getenv("HOME")
	t.Setenv("HOME", dir)

	if err := RegisterActiveJob("uuid-1", "drover-worker-1"); err != nil {
		t.Fatal(err)
	}
	path, err := activeJobsPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"uuid-1"`) || !strings.Contains(string(data), `"drover-worker-1"`) {
		t.Fatalf("active job not persisted: %s", data)
	}

	if err := UnregisterActiveJob("uuid-1"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var jobs map[string]ActiveJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected empty active jobs, got %#v", jobs)
	}

	_ = oldHome
}

func TestReconcileOrphanInstances(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	deleted := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/instances":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"instances":[{"uuid":"orphan-1","name":"drover-worker-9-123"}]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/instances":
			var body deleteInstancesBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body) > 0 {
				deleted[body[0].UUID] = true
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := Config{
		Token:      "token",
		APIBaseURL: srv.URL,
		HTTPClient: srv.Client(),
	}

	n, err := ReconcileOrphanInstances(context.Background(), cfg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted count = %d", n)
	}
	if !deleted["orphan-1"] {
		t.Fatal("expected orphan instance deleted")
	}
}
