//go:build !unix

package docker

import (
	"errors"
	"os/exec"
)

func configureTarget(*exec.Cmd) error {
	return errors.New("container supervisor requires a Unix image")
}
func terminateTargetGroup(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
func killTargetGroup(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}
func configureEscapeChild(*exec.Cmd) error { return errors.New("setsid escape probe requires Linux") }
