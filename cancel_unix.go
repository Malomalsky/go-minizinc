//go:build unix

package minizinc

import (
	"os/exec"
	"syscall"
	"time"
)

// installCooperativeCancel asks the runtime to send SIGTERM when the context
// is cancelled, then escalate to SIGKILL after grace via cmd.WaitDelay.
func installCooperativeCancel(cmd *exec.Cmd, grace time.Duration) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = grace
}
