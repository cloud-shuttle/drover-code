package ukc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const agentListenPort = 8080

// Config holds API credentials and defaults.
type Config struct {
	Token         string
	Metro         string
	DefaultImage  string
	APIBaseURL    string // e.g. https://api.fra.unikraft.cloud
	HTTPClient    *http.Client
	MaxHealthWait time.Duration
}

func apiBaseForMetro(metro string) string {
	metro = strings.TrimSpace(metro)
	if metro == "" {
		metro = "fra"
	}
	return "https://api." + metro + ".unikraft.cloud"
}

// Kraft Cloud platform API uses JSON with snake_case keys.
type createInstanceBody struct {
	Name         string                      `json:"name"`
	Image        string                      `json:"image"`
	Metro        string                      `json:"metro,omitempty"`
	MemoryMB     int                         `json:"memory_mb,omitempty"`
	Env          map[string]string           `json:"env,omitempty"`
	Autostart    bool                        `json:"autostart"`
	ServiceGroup *createInstanceServiceGroup `json:"service_group,omitempty"`
}

type createInstanceServiceGroup struct {
	Services []apiService `json:"services"`
}

type apiService struct {
	Port            uint32   `json:"port"`
	DestinationPort uint32   `json:"destination_port"`
	Handlers        []string `json:"handlers,omitempty"`
}

type createInstanceResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    *struct {
		Instances []apiInstance `json:"instances"`
	} `json:"data"`
}

type apiInstance struct {
	UUID         string           `json:"uuid"`
	Name         string           `json:"name"`
	Metro        string           `json:"metro"`
	ServiceGroup *apiServiceGroup `json:"service_group"`
	PrivateFQDN  string           `json:"private_fqdn"`
}

type apiServiceGroup struct {
	Domains []struct {
		FQDN string `json:"fqdn"`
	} `json:"domains"`
}

type deleteInstancesBody []nameOrUUID

type nameOrUUID struct {
	UUID  string `json:"uuid,omitempty"`
	Name  string `json:"name,omitempty"`
	Metro string `json:"metro,omitempty"`
}

type deleteInstancesResponse struct {
	Status string `json:"status"`
}

// CreateInstance calls POST /v1/instances and returns the first created instance record.
func CreateInstance(ctx context.Context, cfg Config, name, image string, memoryMB int, env map[string]string) (apiInstance, error) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	metro := strings.TrimSpace(cfg.Metro)
	if metro == "" {
		metro = "fra"
	}
	body := createInstanceBody{
		Name:      name,
		Image:     image,
		Metro:     metro,
		Autostart: true,
		ServiceGroup: &createInstanceServiceGroup{
			Services: []apiService{{
				Port:            443,
				DestinationPort: agentListenPort,
				Handlers:        []string{"tls", "http"},
			}},
		},
	}
	if memoryMB > 0 {
		body.MemoryMB = memoryMB
	}
	if len(env) > 0 {
		body.Env = env
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return apiInstance{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.APIBaseURL, "/")+"/v1/instances", bytes.NewReader(raw))
	if err != nil {
		return apiInstance{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return apiInstance{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return apiInstance{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiInstance{}, fmt.Errorf("ukc create instance: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var out createInstanceResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return apiInstance{}, fmt.Errorf("ukc create instance: decode: %w", err)
	}
	if out.Status != "" && out.Status != "success" {
		return apiInstance{}, fmt.Errorf("ukc create instance: status %q: %s", out.Status, out.Message)
	}
	if out.Data == nil || len(out.Data.Instances) == 0 {
		return apiInstance{}, fmt.Errorf("ukc create instance: empty instances in response")
	}
	return out.Data.Instances[0], nil
}

// DeleteInstance removes an instance by UUID.
func DeleteInstance(ctx context.Context, cfg Config, uuid string) error {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	payload := deleteInstancesBody{{UUID: uuid, Metro: cfg.Metro}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(cfg.APIBaseURL, "/")+"/v1/instances", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ukc delete instance: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out deleteInstancesResponse
	_ = json.Unmarshal(respBody, &out)
	if out.Status != "" && out.Status != "success" {
		return fmt.Errorf("ukc delete instance: status %q", out.Status)
	}
	return nil
}

// InstanceHTTPSURL returns a public HTTPS base URL for health and agent calls.
func InstanceHTTPSURL(inst apiInstance) string {
	if inst.ServiceGroup != nil && len(inst.ServiceGroup.Domains) > 0 {
		host := strings.TrimSpace(inst.ServiceGroup.Domains[0].FQDN)
		host = strings.TrimSuffix(host, ".")
		if host != "" {
			return "https://" + host
		}
	}
	if inst.Name != "" && inst.Metro != "" {
		return fmt.Sprintf("https://%s.%s0.unikraft.app", inst.Name, inst.Metro)
	}
	return ""
}
