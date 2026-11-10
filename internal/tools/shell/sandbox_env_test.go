package shell

import (
	"strings"
	"testing"
)

func TestSandboxEnvDropsHostSockets(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1/bus")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent")
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	t.Setenv("HOME", "/Users/me")
	for _, kv := range sandboxEnv(t.TempDir()) {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "DBUS_SESSION_BUS_ADDRESS", "SSH_AUTH_SOCK", "DOCKER_HOST":
			t.Fatalf("kept %s", name)
		}
	}
}

func TestLinuxSandboxArgsHidesHostSockets(t *testing.T) {
	args := linuxSandboxArgs("/work", "/work", "/private/tmp/drover", "true")
	joined := strings.Join(args, "\n")
	for _, dir := range []string{"/tmp", "/run", "/var/run"} {
		if !strings.Contains(joined, "--tmpfs\n"+dir) {
			t.Fatalf("missing tmpfs %s\n%s", dir, joined)
		}
	}
	tmpfsAt := strings.Index(joined, "--tmpfs")
	bindAt := strings.Index(joined, "--bind")
	if tmpfsAt < 0 || bindAt < tmpfsAt {
		t.Fatalf("project bind must follow the tmpfs covers\n%s", joined)
	}
	if !strings.Contains(joined, "--unshare-net") {
		t.Fatal("network namespace missing")
	}
}
