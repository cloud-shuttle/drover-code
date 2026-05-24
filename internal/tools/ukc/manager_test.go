package ukc

import (
	"testing"
)

func TestResolveMetro(t *testing.T) {
	if got := ResolveMetro(""); got != "fra" {
		t.Fatalf("empty = %q", got)
	}
	if got := ResolveMetro("sin"); got != "sin" {
		t.Fatalf("sin = %q", got)
	}
}

func TestMetroFromEnv_prefersMetroOverRegion(t *testing.T) {
	t.Setenv("UKC_METRO", "fra")
	t.Setenv("UKC_REGION", "sin")
	if got := MetroFromEnv(); got != "fra" {
		t.Fatalf("got %q", got)
	}
}

func TestMetroFromEnv_fallsBackToRegion(t *testing.T) {
	t.Setenv("UKC_METRO", "")
	t.Setenv("UKC_REGION", "was")
	if got := MetroFromEnv(); got != "was" {
		t.Fatalf("got %q", got)
	}
}

func TestNewManagerWithCredentials_setsMetroAPIBase(t *testing.T) {
	mgr, err := NewManagerWithCredentials("test-token", "sin", DefaultAgentImage)
	if err != nil {
		t.Fatal(err)
	}
	cfg := mgr.Config()
	if cfg.Metro != "sin" {
		t.Fatalf("metro = %q", cfg.Metro)
	}
	if cfg.APIBaseURL != "https://api.sin.unikraft.cloud" {
		t.Fatalf("api base = %q", cfg.APIBaseURL)
	}
}
