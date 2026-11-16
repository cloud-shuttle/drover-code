package ukc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateInstanceOmitsMetro(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"instances":[{"uuid":"u","name":"n","state":"running"}]}}`))
	}))
	defer srv.Close()

	_, err := CreateInstance(context.Background(), Config{
		Token:      "t",
		Metro:      "syd0-plausible-gnu",
		APIBaseURL: srv.URL,
		HTTPClient: srv.Client(),
	}, "worker", "image:latest", 512, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["metro"]; ok {
		t.Fatalf("body included metro")
	}
}

func TestInstanceHTTPSURL_fallbackMetro(t *testing.T) {
	u := InstanceHTTPSURL(Instance{Name: "my-worker", Metro: "fra"})
	if !strings.HasPrefix(u, "https://") || !strings.Contains(u, ".fra0.unikraft.app") {
		t.Fatalf("got %q", u)
	}
}

func TestInstanceHTTPSURL_prefersDomain(t *testing.T) {
	u := InstanceHTTPSURL(Instance{
		Name:  "x",
		Metro: "fra",
		ServiceGroup: &ServiceGroup{
			Domains: []struct {
				FQDN string `json:"fqdn"`
			}{{FQDN: "custom.example.com."}},
		},
	})
	if u != "https://custom.example.com" {
		t.Fatalf("got %q", u)
	}
}
