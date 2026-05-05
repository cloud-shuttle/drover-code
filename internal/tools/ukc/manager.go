package ukc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultAgentImage is used when ukc_create.image is omitted and UKC_DEFAULT_AGENT_IMAGE is unset.
// Override by publishing your own image and setting the env or passing image explicitly.
const DefaultAgentImage = "index.unikraft.io/cloudshuttle/ukc-agent:latest"

// Manager coordinates registry persistence and Kraft Cloud calls.
type Manager struct {
	mu      sync.Mutex
	regPath string
	entries map[string]Entry
	cfg     Config
}

// NewManagerFromEnv returns a Manager when UKC_TOKEN is set.
func NewManagerFromEnv() (*Manager, bool, error) {
	token := strings.TrimSpace(os.Getenv("UKC_TOKEN"))
	if token == "" {
		return nil, false, nil
	}
	metro := strings.TrimSpace(os.Getenv("UKC_METRO"))
	if metro == "" {
		metro = "fra"
	}
	image := strings.TrimSpace(os.Getenv("UKC_DEFAULT_AGENT_IMAGE"))
	if image == "" {
		image = DefaultAgentImage
	}
	path, err := FilePath()
	if err != nil {
		return nil, false, err
	}
	entries, err := loadRegistry(path)
	if err != nil {
		return nil, false, err
	}

	cfg := Config{
		Token:         token,
		Metro:         metro,
		DefaultImage:  image,
		APIBaseURL:    apiBaseForMetro(metro),
		HTTPClient:    &http.Client{}, // per-call timeouts via context; streams may run until tool timeout
		MaxHealthWait: 60 * time.Second,
	}
	return &Manager{regPath: path, entries: entries, cfg: cfg}, true, nil
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
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for instance health at %s/health", strings.TrimRight(baseURL, "/"))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/health", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if err == nil {
			resp.Body.Close()
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
