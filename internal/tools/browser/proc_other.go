//go:build !darwin && !linux

package browser

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
