//go:build !unix

package docker

import "os/exec"

func configureCLIProcess(*exec.Cmd) error { return nil }
func terminateCLIProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
func killCLIProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
