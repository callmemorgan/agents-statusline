//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const detachedProcess = 0x00000008

func applyDetachSysProcAttr(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess
}
