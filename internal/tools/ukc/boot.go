package ukc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GetInstance fetches one instance by UUID (GET /v1/instances/{uuid}).
// The record can carry env; callers must never log it whole.
func GetInstance(ctx context.Context, cfg Config, uuid string) (Instance, error) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.APIBaseURL, "/")+"/v1/instances/"+uuid, nil)
	if err != nil {
		return Instance{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return Instance{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Instance{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Instance{}, fmt.Errorf("ukc get instance: HTTP %d", resp.StatusCode)
	}
	var out listInstancesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return Instance{}, fmt.Errorf("ukc get instance: decode: %w", err)
	}
	if out.Data == nil || len(out.Data.Instances) == 0 {
		return Instance{}, fmt.Errorf("ukc get instance: not found")
	}
	return out.Data.Instances[0], nil
}

// Stop codes Unikraft Cloud sets itself, before the guest kernel reports one.
const stopCodeNoMedium = 1

// stopped reports whether the instance can no longer serve the worker contract.
func stopped(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "stopped", "stopping", "draining", "deleted", "deleting":
		return true
	}
	return false
}

// DescribeStop turns stop_reason / stop_code / exit_code into one plain line.
// Bits: 0 kernel exited, 1 app exited, 2 platform, 3 user, 4 forced, 5 scale-to-zero.
func DescribeStop(inst Instance) string {
	parts := []string{"state=" + inst.State}
	if inst.StopReason != nil {
		r := *inst.StopReason
		var who []string
		for bit, name := range []string{"kernel exited", "app exited", "platform", "user", "forced", "scale-to-zero"} {
			if r&(1<<bit) != 0 {
				who = append(who, name)
			}
		}
		parts = append(parts, fmt.Sprintf("stop_reason=%d (%s)", r, strings.Join(who, ", ")))
	}
	if inst.StopCode != nil {
		parts = append(parts, fmt.Sprintf("stop_code=%d", *inst.StopCode))
	}
	if inst.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit_code=%d", *inst.ExitCode))
	}
	return strings.Join(parts, " ")
}

// stopHint says what to fix for the common boot failures.
func stopHint(inst Instance) string {
	reason := -1
	if inst.StopReason != nil {
		reason = *inst.StopReason
	}
	platformOnly := reason >= 0 && reason&0b11 == 0 && reason&0b100 != 0
	if (inst.StopCode != nil && *inst.StopCode == stopCodeNoMedium && reason&1 == 0) || platformOnly {
		image := strings.TrimSpace(inst.Image)
		if image == "" {
			image = "the worker image"
		}
		return fmt.Sprintf("the platform stopped it before the app ran; %s is probably missing or failed to pull (publish ukc-agent to this metro)", image)
	}
	if reason >= 0 && reason&0b10 != 0 {
		return "the worker app exited during boot; check its console log"
	}
	if reason >= 0 && reason&0b1 != 0 {
		return "the worker kernel crashed during boot; check its console log"
	}
	return ""
}

// edgeServiceNotFound matches the Unikraft edge page for a host with no service.
func edgeServiceNotFound(status int, body string) bool {
	return status == http.StatusNotFound && strings.Contains(body, "There is no service on this URL")
}

// BootWatch configures WaitForWorker.
type BootWatch struct {
	// MaxWait caps the whole wait. Defaults to 90s.
	MaxWait time.Duration
	// RouteGrace is how long the edge may answer "no service" after the
	// instance reports running before that counts as a misconfigured route.
	// Defaults to 20s.
	RouteGrace time.Duration
	// Poll is the gap between checks. Defaults to 1s.
	Poll time.Duration
	// OnPhase gets "running" and "healthy" with the time since the wait began.
	OnPhase func(phase string, elapsed time.Duration)
}

// WaitForWorker waits for a fresh worker to answer /health, watching the
// instance itself so a dead or unroutable worker fails in seconds instead of
// burning the whole health timeout:
//   - instance stopped during boot: fail now with stop reason, codes and a hint;
//   - instance running but the edge still says "no service" after RouteGrace:
//     fail as a routing misconfiguration (service group or domain).
func WaitForWorker(ctx context.Context, cfg Config, uuid, baseURL, token string, w BootWatch) error {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if w.MaxWait <= 0 {
		w.MaxWait = 90 * time.Second
	}
	if w.RouteGrace <= 0 {
		w.RouteGrace = 20 * time.Second
	}
	if w.Poll <= 0 {
		w.Poll = time.Second
	}
	start := time.Now()
	deadline := start.Add(w.MaxWait)
	healthURL := strings.TrimRight(baseURL, "/") + "/health"
	var runningAt time.Time
	lastErr := "no response"
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if uuid != "" {
			if inst, err := GetInstance(ctx, cfg, uuid); err == nil {
				if stopped(inst.State) {
					msg := fmt.Sprintf("worker instance stopped while booting after %s: %s", time.Since(start).Round(100*time.Millisecond), DescribeStop(inst))
					if hint := stopHint(inst); hint != "" {
						msg += "; " + hint
					}
					return fmt.Errorf("%s", msg)
				}
				if runningAt.IsZero() && strings.EqualFold(inst.State, "running") {
					runningAt = time.Now()
					if w.OnPhase != nil {
						w.OnPhase("running", runningAt.Sub(start))
					}
				}
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := cfg.HTTPClient.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if w.OnPhase != nil {
					w.OnPhase("healthy", time.Since(start))
				}
				return nil
			}
			if edgeServiceNotFound(resp.StatusCode, string(body)) {
				lastErr = "HTTP 404: edge has no service for this host"
				if !runningAt.IsZero() && time.Since(runningAt) > w.RouteGrace {
					return fmt.Errorf("worker route missing: instance running for %s but %s answers \"There is no service on this URL\" (service group port 443 or domain not attached)", time.Since(runningAt).Round(time.Second), healthURL)
				}
			} else {
				lastErr = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
		} else {
			lastErr = err.Error()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s waiting for instance health at %s (last error: %s)", w.MaxWait, healthURL, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.Poll):
		}
	}
}
