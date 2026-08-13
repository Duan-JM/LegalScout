//go:build windows

package worker

import (
	"os/exec"
	"syscall"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

func configureDetached(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}
