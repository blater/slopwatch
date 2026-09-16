package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	args := append([]string{"--follow"}, os.Args[1:]...)
	command := exec.Command(filepath.Join(filepath.Dir(executable), "slopmark"), args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		panic(err)
	}
}
