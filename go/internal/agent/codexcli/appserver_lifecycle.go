package codexcli

import (
	"errors"
	"os/exec"
	"time"
)

func (client *appServerLifecycle) Close(grace time.Duration) error {
	var closeErr error
	client.closeOne.Do(func() {
		client.protocol.appServerWriter.stopOne.Do(func() { close(client.protocol.appServerWriter.stop) })
		_ = client.process.stdin.Close()
		closeErr = errors.Join(closeErr, client.waitForExit(grace))
		closeErr = errors.Join(closeErr, client.stopProcessGroup(grace))
		_ = client.process.stdout.Close()
		closeErr = errors.Join(closeErr, waitForChannel(client.protocol.appServerResponse.done, grace, "Codex App Server output reader did not stop"))
		closeErr = errors.Join(closeErr, waitForChannel(client.protocol.appServerWriter.writerDone, grace, "Codex App Server input writer did not stop"))
	})
	return closeErr
}

func (client *appServerLifecycle) waitForExit(grace time.Duration) error {
	select {
	case <-client.process.waited:
		return nil
	case <-time.After(grace):
		_ = terminateAppServerProcess(client.process.command)
	}
	select {
	case <-client.process.waited:
		return nil
	case <-time.After(grace):
		_ = killAppServerProcess(client.process.command)
		// A detached descendant may retain the inherited stdout descriptor;
		// close our end so the reader and Wait can finish after forced cleanup.
		_ = client.process.stdout.Close()
	}
	select {
	case <-client.process.waited:
		return nil
	case <-time.After(grace):
		return errors.New("Codex App Server process did not stop after forced cancellation")
	}
}

func (client *appServerLifecycle) stopProcessGroup(grace time.Duration) error {
	if !appServerProcessGroupAlive(client.process.command) {
		return nil
	}
	_ = terminateAppServerProcess(client.process.command)
	if waitForProcessGroup(client.process.command, grace) {
		return nil
	}
	_ = killAppServerProcess(client.process.command)
	if waitForProcessGroup(client.process.command, grace) {
		return nil
	}
	return errors.New("Codex App Server process group did not stop after cancellation")
}

func waitForProcessGroup(command *exec.Cmd, grace time.Duration) bool {
	deadline := time.NewTimer(grace)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	defer deadline.Stop()
	for appServerProcessGroupAlive(command) {
		select {
		case <-ticker.C:
		case <-deadline.C:
			return false
		}
	}
	return true
}

func waitForChannel(channel <-chan struct{}, grace time.Duration, message string) error {
	select {
	case <-channel:
		return nil
	case <-time.After(grace):
		return errors.New(message)
	}
}
