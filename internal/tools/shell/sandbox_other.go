//go:build !darwin && !linux

package shell

import (
	"context"
	"fmt"
	"os/exec"
)

func sandboxedCommand(context.Context, string, string, string) (*exec.Cmd, func(), error) {
	return nil, func() {}, fmt.Errorf("bash: sandbox is not available on this system")
}
