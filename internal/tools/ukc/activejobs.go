package ukc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const contractInstancePrefix = "drover-worker-"

// ActiveJob tracks a coordinator-remote agent job while its worker instance is live.
type ActiveJob struct {
	InstanceUUID string    `json:"instance_uuid"`
	InstanceName string    `json:"instance_name"`
	PID          int       `json:"pid"`
	StartedAt    time.Time `json:"started_at"`
}

func activeJobsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".drover-code", "active-agent-jobs.json"), nil
}

func loadActiveJobs(path string) (map[string]ActiveJob, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]ActiveJob), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read active jobs: %w", err)
	}
	var raw map[string]ActiveJob
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse active jobs: %w", err)
	}
	if raw == nil {
		raw = make(map[string]ActiveJob)
	}
	return raw, nil
}

func saveActiveJobs(path string, jobs map[string]ActiveJob) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir active jobs dir: %w", err)
	}
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal active jobs: %w", err)
	}
	data = append(data, '\n')
	return toolutil.WriteAtomic(path, data, 0o600)
}

// RegisterActiveJob records a live contract agent job for orphan reconciliation.
func RegisterActiveJob(instanceUUID, instanceName string) error {
	path, err := activeJobsPath()
	if err != nil {
		return err
	}
	jobs, err := loadActiveJobs(path)
	if err != nil {
		return err
	}
	jobs[instanceUUID] = ActiveJob{
		InstanceUUID: instanceUUID,
		InstanceName: instanceName,
		PID:          os.Getpid(),
		StartedAt:    time.Now().UTC(),
	}
	return saveActiveJobs(path, jobs)
}

// UnregisterActiveJob removes a contract agent job from the active set.
func UnregisterActiveJob(instanceUUID string) error {
	path, err := activeJobsPath()
	if err != nil {
		return err
	}
	jobs, err := loadActiveJobs(path)
	if err != nil {
		return err
	}
	delete(jobs, instanceUUID)
	return saveActiveJobs(path, jobs)
}

type listInstancesResponse struct {
	Status string `json:"status"`
	Data   *struct {
		Instances []Instance `json:"instances"`
	} `json:"data"`
}

// ListInstances returns instances visible to the Kraft Cloud API token.
func ListInstances(ctx context.Context, cfg Config) ([]Instance, error) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.APIBaseURL, "/")+"/v1/instances", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ukc list instances: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out listInstancesResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("ukc list instances: decode: %w", err)
	}
	if out.Data == nil {
		return nil, nil
	}
	return out.Data.Instances, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

// ReconcileOrphanInstances deletes contract worker instances left behind by crashed clients.
// Returns the number of instances deleted.
func ReconcileOrphanInstances(ctx context.Context, cfg Config, maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		maxAge = 2 * time.Hour
	}
	instances, err := ListInstances(ctx, cfg)
	if err != nil {
		return 0, err
	}

	path, err := activeJobsPath()
	if err != nil {
		return 0, err
	}
	active, err := loadActiveJobs(path)
	if err != nil {
		return 0, err
	}

	cloudByUUID := make(map[string]Instance, len(instances))
	for _, inst := range instances {
		if inst.UUID != "" {
			cloudByUUID[inst.UUID] = inst
		}
	}

	deleted := 0
	now := time.Now().UTC()

	for _, inst := range instances {
		if !strings.HasPrefix(inst.Name, contractInstancePrefix) {
			continue
		}
		job, tracked := active[inst.UUID]
		if tracked && processAlive(job.PID) {
			continue
		}
		if err := DeleteInstance(ctx, cfg, inst.UUID); err != nil {
			return deleted, fmt.Errorf("delete orphan %s: %w", inst.Name, err)
		}
		delete(active, inst.UUID)
		deleted++
	}

	for uuid, job := range active {
		if _, ok := cloudByUUID[uuid]; ok {
			continue
		}
		if processAlive(job.PID) {
			continue
		}
		if now.Sub(job.StartedAt) < maxAge {
			continue
		}
		delete(active, uuid)
	}

	if err := saveActiveJobs(path, active); err != nil {
		return deleted, err
	}
	return deleted, nil
}
