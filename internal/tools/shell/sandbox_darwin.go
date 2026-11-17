//go:build darwin

package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func sandboxedCommand(ctx context.Context, project, workDir, command string) (*exec.Cmd, func(), error) {
	root, cwd, err := jailPaths(project, workDir)
	if err != nil {
		return nil, func() {}, err
	}
	bin, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return nil, func() {}, fmt.Errorf("bash: sandbox is not available on this system")
	}
	cleanup, temp, err := privateTemp()
	if err != nil {
		return nil, func() {}, err
	}
	profile := temp + "/profile.sb"
	if err := os.WriteFile(profile, []byte(seatbeltProfile(root, temp)), 0o600); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("bash: sandbox profile: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin, "-f", profile, "bash", "-c", command)
	cmd.Dir = cwd
	cmd.Env = sandboxEnv(temp)
	isolateProcessGroup(cmd)
	return cmd, cleanup, nil
}

func seatbeltProfile(project, temp string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow signal)\n")
	b.WriteString("(allow sysctl-read)\n")
	// ponytail: mach-lookup stays open so libsystem and git work; narrow it when a command reaches the network or home through a system service.
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow ipc-posix*)\n")
	b.WriteString("(allow system-socket)\n")
	b.WriteString("(allow file-read*)\n")
	b.WriteString("(allow file-ioctl)\n")
	fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplQuote(project))
	fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplQuote(temp))
	b.WriteString("(allow file-write-data (literal \"/dev/null\"))\n")
	b.WriteString("(allow file-write-data (literal \"/dev/dtracehelper\"))\n")
	b.WriteString("(deny network*)\n")
	return b.String()
}

func sbplQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
