//go:build unix

package codexcli

import (
	"os/exec"
	"syscall"
)

func configureAppServerProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateAppServerProcess(command *exec.Cmd) error {
	return syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
}

func killAppServerProcess(command *exec.Cmd) error {
	return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}

func appServerProcessGroupAlive(command *exec.Cmd) bool {
	return syscall.Kill(-command.Process.Pid, 0) == nil
}
