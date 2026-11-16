//go:build darwin || linux

package shell

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// isolateProcessGroup puts the command in its own group so a timeout kills
// the sandbox helper and the processes it starts.
// ponytail: a command that calls setpgid can outlive the timeout; kill that group when a job escapes.
func isolateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
}
