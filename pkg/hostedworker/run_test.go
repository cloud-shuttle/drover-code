package hostedworker

import (
	"context"
	"os"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/tools/ukc"
)

func TestRun_NoToken(t *testing.T) {
	os.Unsetenv("UKC_TOKEN")
	_, err := Run(context.Background(), RunInput{})
	if err == nil {
		t.Error("expected error for missing UKC_TOKEN")
	}
}

func TestManagerForRun(t *testing.T) {
	// Without token
	os.Unsetenv("UKC_TOKEN")
	_, err := managerForRun(RunInput{})
	if err == nil {
		t.Error("expected error")
	}

	// With token
	_, err = managerForRun(RunInput{Token: "test-tok"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// With token in env
	os.Setenv("UKC_TOKEN", "test-env-tok")
	defer os.Unsetenv("UKC_TOKEN")
	_, err = managerForRun(RunInput{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEmitIfPossible(t *testing.T) {
	err := emitIfPossible(nil, "status", "test")
	if err != nil {
		t.Error(err)
	}

	called := false
	cb := func(s string) {
		called = true
		if s != `{"stream":"status","line":"test"}` {
			t.Errorf("unexpected string: %s", s)
		}
	}
	err = emitIfPossible(cb, "status", "test")
	if err != nil {
		t.Error(err)
	}
	if !called {
		t.Error("callback not called")
	}
}

func TestCleanupInstance_Timeout(t *testing.T) {
	// context canceled immediately should return context error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Since we mock nothing, the underlying HTTP call in ukc.DeleteInstance will just return ctx.Err()
	// and our retry loop will bail out
	err := cleanupInstance(ctx, ukc.Config{}, "uuid-1")
	if err == nil {
		t.Error("expected error from canceled context")
	}
}
