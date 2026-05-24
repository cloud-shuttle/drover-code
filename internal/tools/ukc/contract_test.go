package ukc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	c := New("http://example.com/", "token", nil)
	if c.BaseURL != "http://example.com" {
		t.Errorf("expected trimmed base url, got %q", c.BaseURL)
	}
	if c.HTTP != http.DefaultClient {
		t.Errorf("expected default client")
	}
}

func TestRunContract_FailHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := New(ts.URL, "tok", ts.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	spec := ContractSpec{
		MaxHealthWait: 50 * time.Millisecond,
	}

	_, err := RunContract(ctx, c, spec)
	if err == nil {
		t.Fatal("expected health check failure")
	}
}
