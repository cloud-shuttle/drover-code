//go:build linux

package shell

import (
	"context"
	"fmt"
	"os/exec"
)

func sandboxedCommand(ctx context.Context, project, workDir, command string) (*exec.Cmd, func(), error) {
	root, cwd, err := jailPaths(project, workDir)
	if err != nil {
		return nil, func() {}, err
	}
	bin, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, func() {}, fmt.Errorf("bash: sandbox is not available on this system")
	}
	cleanup, temp, err := privateTemp()
	if err != nil {
		return nil, func() {}, err
	}
	cmd := exec.CommandContext(ctx, bin, linuxSandboxArgs(root, cwd, temp, command)...)
	cmd.Dir = cwd
	cmd.Env = sandboxEnv(temp)
	isolateProcessGroup(cmd)
	return cmd, cleanup, nil
}
