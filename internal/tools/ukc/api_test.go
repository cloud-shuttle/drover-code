package ukc

import (
	"strings"
	"testing"
)

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
