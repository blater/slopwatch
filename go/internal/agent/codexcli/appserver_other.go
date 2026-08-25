//go:build !unix

package codexcli

import "os/exec"

func configureAppServerProcess(*exec.Cmd) {}

func terminateAppServerProcess(command *exec.Cmd) error { return command.Process.Kill() }

func killAppServerProcess(command *exec.Cmd) error { return command.Process.Kill() }

func appServerProcessGroupAlive(*exec.Cmd) bool { return false }
