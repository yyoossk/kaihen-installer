//go:build windows

package main

import "os/exec"

// setProcGroup is a no-op on Windows; killProcessTree uses taskkill /T
// there instead.
func setProcGroup(cmd *exec.Cmd) {}

// killProcGroup is unused on Windows (taskkill handles the tree).
func killProcGroup(pid int) bool { return false }
