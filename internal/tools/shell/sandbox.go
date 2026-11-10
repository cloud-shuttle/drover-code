package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// command starts bash -c. A sandboxed command can write only inside the
// project and a private temp directory, and it cannot use the network.
func (t *Bash) command(ctx context.Context, workDir, command string) (*exec.Cmd, func(), error) {
	if t == nil || !t.Sandbox {
		cmd := exec.CommandContext(ctx, "bash", "-c", command)
		cmd.Dir = workDir
		return cmd, func() {}, nil
	}
	project := t.WorkDir
	if project == "" {
		project = workDir
	}
	return sandboxedCommand(ctx, project, workDir, command)
}

func jailPaths(project, workDir string) (root, cwd string, err error) {
	if workDir == "" {
		workDir = project
	}
	if project == "" {
		return "", "", fmt.Errorf("bash: sandbox requires a working directory")
	}
	root, err = realDir(project)
	if err != nil {
		return "", "", err
	}
	cwd, err = realDir(workDir)
	if err != nil {
		return "", "", err
	}
	if cwd != root && !strings.HasPrefix(cwd, root+string(filepath.Separator)) {
		return "", "", fmt.Errorf("bash: working directory escapes the project")
	}
	return root, cwd, nil
}

func realDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("bash: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("bash: working directory: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("bash: working directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("bash: working directory is not a directory")
	}
	if strings.ContainsAny(real, "\n\x00") {
		return "", fmt.Errorf("bash: working directory contains a character the sandbox cannot quote")
	}
	return real, nil
}

func privateTemp() (func(), string, error) {
	temp, err := os.MkdirTemp("", "drover-bash-")
	if err != nil {
		return func() {}, "", fmt.Errorf("bash: sandbox temp: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(temp) }
	real, err := filepath.EvalSymlinks(temp)
	if err != nil {
		cleanup()
		return func() {}, "", fmt.Errorf("bash: sandbox temp: %w", err)
	}
	if strings.ContainsAny(real, "\n\x00") {
		cleanup()
		return func() {}, "", fmt.Errorf("bash: sandbox temp contains a character the sandbox cannot quote")
	}
	if err := os.MkdirAll(filepath.Join(real, "gocache"), 0o700); err != nil {
		cleanup()
		return func() {}, "", fmt.Errorf("bash: sandbox temp: %w", err)
	}
	return cleanup, real, nil
}

// ponytail: GOCACHE is a fresh directory per command; reuse one cache per session when sandboxed builds become the slow path.
func sandboxEnv(temp string) []string {
	overrides := map[string]string{
		"TMPDIR":      temp,
		"TMP":         temp,
		"TEMP":        temp,
		"GOCACHE":     filepath.Join(temp, "gocache"),
		"GOTMPDIR":    temp,
		"GOENV":       "off",
		"GOTELEMETRY": "off",
	}
	base := os.Environ()
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, drop := overrides[name]; drop || sandboxDropEnv(name) {
			continue
		}
		out = append(out, kv)
	}
	for name, value := range overrides {
		out = append(out, name+"="+value)
	}
	return out
}

func sandboxDropEnv(name string) bool {
	switch name {
	case "DBUS_SESSION_BUS_ADDRESS", "SSH_AUTH_SOCK", "DOCKER_HOST":
		return true
	default:
		return false
	}
}

// linuxSandboxArgs is the bwrap command line. Host runtime sockets under
// /tmp, /run, and /var/run are covered before the project and temp binds.
func linuxSandboxArgs(root, cwd, temp, command string) []string {
	return []string{
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp",
		"--tmpfs", "/run",
		"--tmpfs", "/var/run",
		"--bind", root, root,
		"--bind", temp, temp,
		"--dev", "/dev",
		"--proc", "/proc",
		"--unshare-net",
		"--die-with-parent",
		"--chdir", cwd,
		"--",
		"bash", "-c", command,
	}
}
