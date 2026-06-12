//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func applyDetachSysProcAttr(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setsid = true
}
