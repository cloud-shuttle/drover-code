package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAllowedURL(t *testing.T) {
	if err := allowedURL("file:///etc/passwd"); err == nil {
		t.Fatal("file url was accepted")
	}
	if err := allowedURL("https://example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestBrowser_OpenRequiresChromeAndReadsPage(t *testing.T) {
	if _, err := findChrome(); err != nil {
		t.Skip(err.Error())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><title>Hello</title><body><p id="msg">pong</p><input id="q"></body>`))
	}))
	defer srv.Close()

	b := &Browser{}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := b.Execute(ctx, []byte(`{"action":"open","url":"`+srv.URL+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "pong") || !strings.Contains(out, "Hello") {
		t.Fatalf("snapshot: %s", out)
	}

	out, err = b.Execute(ctx, []byte(`{"action":"type","selector":"#q","text":"hi"}`))
	if err != nil || out != "typed" {
		t.Fatalf("type %q %v", out, err)
	}
	out, err = b.Execute(ctx, []byte(`{"action":"click","selector":"#missing"}`))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("click err %v", err)
	}
	out, err = b.Execute(ctx, []byte(`{"action":"screenshot"}`))
	if err != nil || !strings.Contains(out, "@@image image/png ") {
		t.Fatalf("screenshot %v len %d", err, len(out))
	}
	out, err = b.Execute(ctx, []byte(`{"action":"close"}`))
	if err != nil || out != "browser closed" {
		t.Fatalf("close %q %v", out, err)
	}
}

func TestBrowser_RejectsFileURLBeforeLaunch(t *testing.T) {
	b := &Browser{}
	_, err := b.Execute(context.Background(), []byte(`{"action":"open","url":"file:///etc/passwd"}`))
	if err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatal(err)
	}
	if b.sess != nil {
		t.Fatal("chrome started for a rejected url")
	}
}
