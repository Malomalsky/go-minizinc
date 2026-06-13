//go:build !unix

package minizinc

import (
	"os/exec"
	"time"
)

// installCooperativeCancel on non-Unix platforms (notably Windows) falls back
// to the exec default — context cancellation calls cmd.Process.Kill. Posix
// signals are not portable here.
func installCooperativeCancel(cmd *exec.Cmd, grace time.Duration) {
	cmd.WaitDelay = grace
}
