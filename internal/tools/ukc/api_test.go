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
	sg, _ := got["service_group"].(map[string]any)
	domains, _ := sg["domains"].([]any)
	if len(domains) != 1 {
		t.Fatalf("domains: %#v", sg["domains"])
	}
	domain, _ := domains[0].(map[string]any)
	if domain["name"] != "worker" {
		t.Fatalf("domain name: %#v", domain["name"])
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
			Domains: []ServiceDomain{{FQDN: "custom.example.com."}},
		},
	})
	if u != "https://custom.example.com" {
		t.Fatalf("got %q", u)
	}
}

func TestInstanceHTTPSURL_usesDomainNameWhenFQDNBlank(t *testing.T) {
	u := InstanceHTTPSURL(Instance{
		Name:  "drover-hosted-aj",
		Metro: "syd0-plausible-gnu",
		FQDN:  "small-tree-d1uu53wa.syd0-plausible-gnu.unikraft.app",
		ServiceGroup: &ServiceGroup{
			Name: "small-tree-d1uu53wa",
			Domains: []ServiceDomain{{
				Name: "drover-hosted-aj.syd0-plausible-gnu.unikraft.app",
			}},
		},
	})
	if u != "https://drover-hosted-aj.syd0-plausible-gnu.unikraft.app" {
		t.Fatalf("got %q", u)
	}
}

func TestInstanceHTTPSURL_skipsServiceGroupName(t *testing.T) {
	u := InstanceHTTPSURL(Instance{
		Name:  "drover-hosted-aj-1795005027246934169",
		Metro: "https://api.syd0-plausible-gnu.unikraft.cloud",
		FQDN:  "small-tree-d1uu53wa.syd0-plausible-gnu.unikraft.app",
		ServiceGroup: &ServiceGroup{
			Name: "small-tree-d1uu53wa",
			Domains: []ServiceDomain{{
				FQDN: "small-tree-d1uu53wa.syd0-plausible-gnu.unikraft.app",
			}},
		},
	})
	if u != "https://drover-hosted-aj-1795005027246934169.syd0-plausible-gnu.unikraft.app" {
		t.Fatalf("got %q", u)
	}
}

func TestInstanceHTTPSURL_sydneyMetroKeepsFullID(t *testing.T) {
	u := InstanceHTTPSURL(Instance{
		Name:  "drover-hosted-aj",
		Metro: "syd0-plausible-gnu",
	})
	if u != "https://drover-hosted-aj.syd0-plausible-gnu.unikraft.app" {
		t.Fatalf("got %q", u)
	}
}
