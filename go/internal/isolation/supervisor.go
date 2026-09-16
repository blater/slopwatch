package isolation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

const (
	supervisorCanceledExit = 130
	supervisorTimeoutExit  = 124
	maximumRequestBytes    = 16 << 20
)

// SupervisorMain handles the hidden self-reexec entry point. Callers must run
// it before ordinary flag parsing and exit with the returned code when handled.
func SupervisorMain(arguments []string) (handled bool, exitCode int) {
	if len(arguments) != 1 || arguments[0] != SupervisorArgument {
		return false, 0
	}
	return true, serveSupervisor()
}

func serveSupervisor() int {
	control := os.NewFile(3, "slopwatch-supervisor-control")
	requestFile := os.NewFile(4, "slopwatch-supervisor-request")
	if control == nil || requestFile == nil {
		fmt.Fprintln(os.Stderr, ErrSupervisorProtocol)
		return 125
	}
	defer control.Close()
	defer requestFile.Close()
	limited := io.LimitReader(requestFile, maximumRequestBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil || len(payload) > maximumRequestBytes {
		fmt.Fprintln(os.Stderr, ErrSupervisorProtocol)
		return 125
	}
	var request supervisorRequest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		fmt.Fprintf(os.Stderr, "%v: %v\n", ErrSupervisorProtocol, err)
		return 125
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		fmt.Fprintln(os.Stderr, ErrSupervisorProtocol)
		return 125
	}
	if request.Executable == "" || request.Directory == "" || request.WallTimeNanos < 0 || request.TerminateGraceNanos <= 0 {
		fmt.Fprintln(os.Stderr, ErrSupervisorProtocol)
		return 125
	}

	command := exec.Command(request.Executable, request.Arguments...)
	command.Dir = request.Directory
	command.Env = append([]string(nil), request.Environment...)
	command.Stdin = bytes.NewReader(request.Stdin)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := configureProcessGroup(command); err != nil {
		fmt.Fprintf(os.Stderr, "configure child process group: %v\n", err)
		return 125
	}
	if err := command.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start isolated child: %v\n", err)
		return 125
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	parentGone := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(io.Discard, control)
		parentGone <- struct{}{}
	}()
	var timeout <-chan time.Time
	var timer *time.Timer
	if request.WallTimeNanos > 0 {
		timer = time.NewTimer(time.Duration(request.WallTimeNanos))
		timeout = timer.C
		defer timer.Stop()
	}
	select {
	case err := <-waited:
		cleanupRemainingProcessGroup(command.Process.Pid, time.Duration(request.TerminateGraceNanos))
		return exitCode(err)
	case <-parentGone:
		terminateAndWait(command, waited, time.Duration(request.TerminateGraceNanos))
		return supervisorCanceledExit
	case <-timeout:
		terminateAndWait(command, waited, time.Duration(request.TerminateGraceNanos))
		return supervisorTimeoutExit
	}
}

func terminateAndWait(command *exec.Cmd, waited <-chan error, grace time.Duration) {
	if command.Process == nil {
		return
	}
	_ = terminateProcessGroup(command.Process.Pid)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	leaderExited := false
	select {
	case <-waited:
		leaderExited = true
	case <-timer.C:
	}
	_ = killProcessGroup(command.Process.Pid)
	if leaderExited {
		return
	}
	select {
	case <-waited:
	case <-time.After(grace):
		_ = command.Process.Kill()
		select {
		case <-waited:
		case <-time.After(grace):
		}
	}
}

func cleanupRemainingProcessGroup(pid int, grace time.Duration) {
	if !processGroupAlive(pid) {
		return
	}
	_ = terminateProcessGroup(pid)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	<-timer.C
	_ = killProcessGroup(pid)
}
