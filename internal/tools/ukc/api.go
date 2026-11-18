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
	if strings.HasPrefix(metro, "http://") || strings.HasPrefix(metro, "https://") {
		metro = strings.TrimRight(metro, "/")
		return strings.TrimSuffix(metro, "/v1")
	}
	// Also catch cases where they might have accidentally forgotten the colon
	if strings.HasPrefix(metro, "http//") || strings.HasPrefix(metro, "https//") {
		metro = strings.Replace(metro, "//", "://", 1)
		metro = strings.TrimRight(metro, "/")
		return strings.TrimSuffix(metro, "/v1")
	}
	return "https://api." + metro + ".unikraft.cloud"
}

// Kraft Cloud platform API uses JSON with snake_case keys.
type createInstanceBody struct {
	Name         string                      `json:"name"`
	Image        string                      `json:"image"`
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
		Instances []Instance `json:"instances"`
	} `json:"data"`
}

type Instance struct {
	UUID         string        `json:"uuid"`
	Name         string        `json:"name"`
	State        string        `json:"state"`
	Metro        string        `json:"metro"`
	ServiceGroup *ServiceGroup `json:"service_group"`
	PrivateFQDN  string        `json:"private_fqdn"`
	FQDN         string        `json:"fqdn"`
}

type ServiceGroup struct {
	UUID    string          `json:"uuid"`
	Name    string          `json:"name"`
	Domains []ServiceDomain `json:"domains"`
}

// ServiceDomain is one public name on a service group.
// Create responses sometimes set name and leave fqdn empty.
type ServiceDomain struct {
	FQDN string `json:"fqdn"`
	Name string `json:"name"`
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
func CreateInstance(ctx context.Context, cfg Config, name, image string, memoryMB int, env map[string]string) (Instance, error) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	// The API host is already the metro. This API rejects a metro field in the body.
	body := createInstanceBody{
		Name:      name,
		Image:     image,
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
		return Instance{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.APIBaseURL, "/")+"/v1/instances", bytes.NewReader(raw))
	if err != nil {
		return Instance{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return Instance{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Instance{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Instance{}, fmt.Errorf("ukc create instance: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var out createInstanceResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return Instance{}, fmt.Errorf("ukc create instance: decode: %w", err)
	}
	if out.Status != "" && out.Status != "success" {
		return Instance{}, fmt.Errorf("ukc create instance: status %q: %s", out.Status, out.Message)
	}
	if out.Data == nil || len(out.Data.Instances) == 0 {
		return Instance{}, fmt.Errorf("ukc create instance: empty instances in response")
	}
	return out.Data.Instances[0], nil
}

// GetServiceGroup fetches a service group by UUID to retrieve its domains.
func GetServiceGroup(ctx context.Context, cfg Config, uuid string) (ServiceGroup, error) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.APIBaseURL, "/")+"/v1/services/"+uuid, nil)
	if err != nil {
		return ServiceGroup{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return ServiceGroup{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ServiceGroup{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ServiceGroup{}, fmt.Errorf("ukc get service group: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var out struct {
		Status string `json:"status"`
		Data   struct {
			ServiceGroups []ServiceGroup `json:"services"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ServiceGroup{}, err
	}
	if len(out.Data.ServiceGroups) == 0 {
		return ServiceGroup{}, fmt.Errorf("service group not found")
	}
	return out.Data.ServiceGroups[0], nil
}

// DeleteInstance removes an instance by UUID.
func DeleteInstance(ctx context.Context, cfg Config, uuid string) error {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	payload := deleteInstancesBody{{UUID: uuid}}
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

// StopInstance requests Kraft Cloud to stop (power off) the given instance.
// This is useful as a "force stop" step when normal DeleteInstance hangs because
// some UKC instances can be stubborn while still running. The stop is best-effort;
// caller should still attempt Delete afterward. Uses the same bulk payload shape
// as delete, posted to the /stop action endpoint.
func StopInstance(ctx context.Context, cfg Config, uuid string) error {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	payload := deleteInstancesBody{{UUID: uuid}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.APIBaseURL, "/")+"/v1/instances/stop", bytes.NewReader(raw))
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
	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		// already gone or never existed — treat as success for cleanup purposes
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ukc stop instance: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out deleteInstancesResponse
	_ = json.Unmarshal(respBody, &out)
	if out.Status != "" && out.Status != "success" {
		// some errors are transient; surface but caller decides
		return fmt.Errorf("ukc stop instance: status %q", out.Status)
	}
	return nil
}

// MetroID reduces a metro flag or API origin to the metro id.
// https://api.syd0-plausible-gnu.unikraft.cloud and syd0-plausible-gnu both become syd0-plausible-gnu.
func MetroID(metro string) string {
	metro = strings.TrimSpace(metro)
	metro = strings.TrimPrefix(metro, "https://")
	metro = strings.TrimPrefix(metro, "http://")
	metro = strings.TrimPrefix(metro, "api.")
	metro = strings.TrimSuffix(metro, "/v1")
	metro = strings.TrimSuffix(metro, ".unikraft.cloud")
	return strings.Trim(metro, "/")
}

// publicMetro is the label used in unikraft.app hostnames.
// Short legacy ids such as fra become fra0. Full ids such as syd0-plausible-gnu stay as they are.
func publicMetro(metro string) string {
	metro = MetroID(metro)
	if metro == "" {
		return ""
	}
	if strings.Contains(metro, "-") || strings.ContainsAny(metro, "0123456789") {
		return metro
	}
	return metro + "0"
}

func (d ServiceDomain) host(metro string) string {
	host := strings.TrimSpace(d.FQDN)
	if host == "" {
		host = strings.TrimSpace(d.Name)
	}
	host = strings.TrimSuffix(host, ".")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if host == "" {
		return ""
	}
	if !strings.Contains(host, ".") {
		pm := publicMetro(metro)
		if pm == "" {
			return ""
		}
		return host + "." + pm + ".unikraft.app"
	}
	return host
}

// serviceGroupHost reports whether host is the unpublished service-group name.
// Unikraft publishes {instance}.{metro}.unikraft.app. The group name
// (small-tree-d1uu53wa) answers "There is no service on this URL."
func serviceGroupHost(host, groupName, metro string) bool {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return false
	}
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if host == groupName {
		return true
	}
	for _, label := range []string{publicMetro(metro), MetroID(metro)} {
		if label != "" && host == groupName+"."+label+".unikraft.app" {
			return true
		}
	}
	return false
}

// publishedHost is the hostname that actually receives TLS on port 443.
func publishedHost(inst Instance) string {
	groupName := ""
	if inst.ServiceGroup != nil {
		groupName = inst.ServiceGroup.Name
		for _, domain := range inst.ServiceGroup.Domains {
			host := domain.host(inst.Metro)
			if host == "" || serviceGroupHost(host, groupName, inst.Metro) {
				continue
			}
			return host
		}
	}
	if fqdn := strings.TrimSuffix(strings.TrimSpace(inst.FQDN), "."); fqdn != "" && !serviceGroupHost(fqdn, groupName, inst.Metro) {
		fqdn = strings.TrimPrefix(fqdn, "https://")
		fqdn = strings.TrimPrefix(fqdn, "http://")
		if fqdn != "" {
			return fqdn
		}
	}
	name := strings.TrimSpace(inst.Name)
	pm := publicMetro(inst.Metro)
	if name != "" && pm != "" {
		return name + "." + pm + ".unikraft.app"
	}
	return ""
}

func withScheme(host string) string {
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "https://" + host
}

// InstanceHTTPSURL returns a public HTTPS base URL for health and agent calls.
func InstanceHTTPSURL(inst Instance) string {
	return withScheme(publishedHost(inst))
}

// needsServiceGroupRefresh is true when the create response has a service
// group but no published domain yet. A non-empty domains list can still be
// unusable when each entry leaves fqdn blank.
func needsServiceGroupRefresh(inst Instance) bool {
	if inst.ServiceGroup == nil || strings.TrimSpace(inst.ServiceGroup.UUID) == "" {
		return false
	}
	groupName := inst.ServiceGroup.Name
	for _, domain := range inst.ServiceGroup.Domains {
		host := domain.host(inst.Metro)
		if host != "" && !serviceGroupHost(host, groupName, inst.Metro) {
			return false
		}
	}
	return true
}

// ResolvePublicURL loads the service-group domain when the create response
// omitted it, then returns the HTTPS origin used for /health and the worker contract.
func ResolvePublicURL(ctx context.Context, cfg Config, inst Instance) (string, error) {
	if strings.TrimSpace(inst.Metro) == "" {
		inst.Metro = MetroID(cfg.Metro)
	}
	if needsServiceGroupRefresh(inst) {
		sg, err := GetServiceGroup(ctx, cfg, inst.ServiceGroup.UUID)
		if err == nil {
			if strings.TrimSpace(sg.Name) == "" {
				sg.Name = inst.ServiceGroup.Name
			}
			inst.ServiceGroup = &sg
		}
	}
	url := InstanceHTTPSURL(inst)
	if url == "" {
		return "", fmt.Errorf("worker instance has no public URL")
	}
	return url, nil
}
