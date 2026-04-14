package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_PostIssueComment_roundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/issues/3/comments" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"body":"hi"`) {
			t.Fatalf("body %s", b)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 42}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient("tok")
	c.apiBaseURL = srv.URL

	id, err := c.PostIssueComment(context.Background(), "o", "r", 3, "hi")
	if err != nil || id != 42 {
		t.Fatalf("id=%d err=%v", id, err)
	}
}

func TestClient_UpdateComment_roundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/o/r/issues/comments/9" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := NewClient("tok")
	c.apiBaseURL = srv.URL
	if err := c.UpdateComment(context.Background(), "o", "r", 9, "done"); err != nil {
		t.Fatal(err)
	}
}

func TestClient_PostIssueComment_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := NewClient("tok")
	c.apiBaseURL = srv.URL
	_, err := c.PostIssueComment(context.Background(), "o", "r", 1, "x")
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("err=%v", err)
	}
}

func TestClient_GetIssue_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient("tok")
	c.apiBaseURL = srv.URL
	_, err := c.GetIssue(context.Background(), "o", "r", 99)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("err=%v", err)
	}
}

func TestClient_ListIssueComments_ok(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/1/comments" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"body":"a"}]`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient("tok")
	c.apiBaseURL = srv.URL
	list, err := c.ListIssueComments(context.Background(), "o", "r", 1)
	if err != nil || len(list) != 1 || list[0].Body != "a" {
		t.Fatalf("%+v %v", list, err)
	}
}
