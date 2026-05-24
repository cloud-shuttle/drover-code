package guardclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Evaluate(t *testing.T) {
	c := NewClient("", "")
	_, err := c.Evaluate(context.Background(), EvaluateRequest{})
	if err == nil {
		t.Fatal("expected error for empty base URL")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"allowed":true}`))
	}))
	defer ts.Close()

	c = NewClient(ts.URL, "token")
	resp, err := c.Evaluate(context.Background(), EvaluateRequest{
		TenantID:     "t-1",
		AgentID:      "a-1",
		Action:       "read",
		ResourceType: "file",
		ResourceID:   "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Allowed {
		t.Fatal("expected allowed")
	}

	// Test error status code
	tsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer tsErr.Close()

	cErr := NewClient(tsErr.URL, "token")
	_, err = cErr.Evaluate(context.Background(), EvaluateRequest{})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}
