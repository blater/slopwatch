//go:build !unix

package isolation

import (
	"os"
	"os/exec"
)

func configureProcessGroup(*exec.Cmd) error { return nil }

func terminateProcessGroup(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func killProcessGroup(pid int) error { return terminateProcessGroup(pid) }

func processGroupAlive(int) bool { return false }
