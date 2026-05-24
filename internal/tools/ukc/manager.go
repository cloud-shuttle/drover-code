package ukc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultAgentImage is used when ukc_create.image is omitted and UKC_DEFAULT_AGENT_IMAGE is unset.
// Override by publishing your own image and setting the env or passing image explicitly.
const DefaultAgentImage = "index.unikraft.io/cloudshuttle/ukc-agent:latest"

// Manager coordinates registry persistence and Kraft Cloud calls.
type Manager struct {
	mu        sync.Mutex
	regPath   string
	entries   map[string]Entry
	cfg       Config
	Templates *TemplatesCache
}

// MetroFromEnv returns the Kraftcloud metro/region from UKC_METRO or UKC_REGION.
func MetroFromEnv() string {
	return ResolveMetro(firstNonEmpty(
		os.Getenv("UKC_METRO"),
		os.Getenv("UKC_REGION"),
	))
}

// ResolveMetro normalizes a Kraftcloud metro code (default fra).
func ResolveMetro(metro string) string {
	metro = strings.TrimSpace(metro)
	if metro == "" {
		return "fra"
	}
	return metro
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// NewManagerFromEnv returns a Manager when UKC_TOKEN is set.
func NewManagerFromEnv() (*Manager, bool, error) {
	token := strings.TrimSpace(os.Getenv("UKC_TOKEN"))
	if token == "" {
		return nil, false, nil
	}
	mgr, err := NewManagerWithCredentials(token, MetroFromEnv(), strings.TrimSpace(os.Getenv("UKC_DEFAULT_AGENT_IMAGE")))
	if err != nil {
		return nil, false, err
	}
	return mgr, true, nil
}

// NewManagerWithCredentials builds a Manager for hosted execution clients.
func NewManagerWithCredentials(token, metro, image string) (*Manager, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("ukc: token required")
	}
	metro = ResolveMetro(metro)
	if image = strings.TrimSpace(image); image == "" {
		image = DefaultAgentImage
	}
	path, err := FilePath()
	if err != nil {
		return nil, err
	}
	entries, err := loadRegistry(path)
	if err != nil {
		return nil, err
	}

	templatesPath := filepath.Join(filepath.Dir(path), "ukc-templates.json")
	templatesCache, err := NewTemplatesCache(templatesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load templates cache: %w", err)
	}

	cfg := Config{
		Token:         token,
		Metro:         metro,
		DefaultImage:  image,
		APIBaseURL:    apiBaseForMetro(metro),
		HTTPClient:    &http.Client{},
		MaxHealthWait: 60 * time.Second,
	}
	return &Manager{regPath: path, entries: entries, cfg: cfg, Templates: templatesCache}, nil
}

func (m *Manager) persistLocked() error {
	return saveRegistry(m.regPath, m.entries)
}

func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// RandToken returns a random 32-byte hex token for AGENT_TOKEN.
func RandToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// WaitForHealth polls GET /health until 200 or ctx done.
func WaitForHealth(ctx context.Context, client *http.Client, baseURL, token string, maxWait time.Duration) error {
	if client == nil {
		client = http.DefaultClient
	}
	deadline := time.Now().Add(maxWait)
	backoff := 200 * time.Millisecond
	var lastErr string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			if lastErr == "" {
				lastErr = "no response"
			}
			return fmt.Errorf("timeout waiting for instance health at %s/health (last error: %s)", strings.TrimRight(baseURL, "/"), lastErr)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/health", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return nil
			}
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			lastErr = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			resp.Body.Close()
		} else {
			lastErr = err.Error()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 2*time.Second {
			backoff += 200 * time.Millisecond
		}
	}
}
