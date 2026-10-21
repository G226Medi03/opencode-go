//go:build windows

package proc

import (
	"os/exec"
	"strconv"
)

// Configure is a no-op on Windows; children are killed by PID via taskkill.
func Configure(_ *exec.Cmd) {}

// KillTree terminates a process and its children using taskkill.
func KillTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	_ = exec.Command("taskkill", "/T", "/F", "/PID", pid).Run()
}
