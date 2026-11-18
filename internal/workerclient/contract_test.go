package workerclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunContract_FailHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := New(ts.URL, "tok", ts.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := RunContract(ctx, c, ContractSpec{
		MaxHealthWait: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected health check failure")
	}
}
