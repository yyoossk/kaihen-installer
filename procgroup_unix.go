//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setProcGroup starts the command in its own process group so the whole
// tree (the Node engine spawns children through process.execPath) can be
// killed at once.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcGroup SIGKILLs the process group led by pid. It reports whether
// the group signal was delivered.
func killProcGroup(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(-pid, syscall.SIGKILL) == nil
}
