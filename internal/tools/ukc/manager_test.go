package ukc

import (
	"testing"
)

func TestResolveMetro(t *testing.T) {
	if DefaultMetro != "syd0" {
		t.Fatalf("DefaultMetro = %q, want syd0", DefaultMetro)
	}
	if got := ResolveMetro(""); got != "syd0" {
		t.Fatalf("empty = %q, want syd0", got)
	}
	if got := ResolveMetro("sin"); got != "sin" {
		t.Fatalf("sin = %q", got)
	}
	if got := ResolveMetro("fra"); got != "fra" {
		t.Fatalf("explicit fra must be kept, got %q", got)
	}
	if got := ResolveMetro("syd0-plausible-gnu"); got != "syd0-plausible-gnu" {
		t.Fatalf("full sydney id = %q", got)
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

func TestMetroFromEnv_defaultsToSydney(t *testing.T) {
	t.Setenv("UKC_METRO", "")
	t.Setenv("UKC_REGION", "")
	if got := MetroFromEnv(); got != "syd0" {
		t.Fatalf("got %q, want syd0", got)
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

func TestNewManagerWithCredentials_defaultsToSydney(t *testing.T) {
	mgr, err := NewManagerWithCredentials("test-token", "", DefaultAgentImage)
	if err != nil {
		t.Fatal(err)
	}
	cfg := mgr.Config()
	if cfg.Metro != "syd0" {
		t.Fatalf("metro = %q", cfg.Metro)
	}
	if cfg.APIBaseURL != "https://api.syd0.unikraft.cloud" {
		t.Fatalf("api base = %q", cfg.APIBaseURL)
	}
}
