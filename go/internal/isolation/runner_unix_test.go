//go:build unix

package isolation

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestIsolationHelper(t *testing.T) {
	marker := argumentAfterDoubleDash(os.Args)
	if len(marker) == 0 {
		return
	}
	if handled, code := SupervisorMain(marker); handled {
		os.Exit(code)
	}
	switch marker[0] {
	case "output":
		fmt.Fprint(os.Stdout, strings.Repeat("o", 128))
		fmt.Fprint(os.Stderr, strings.Repeat("e", 64))
		os.Exit(0)
	case "stream":
		fmt.Fprintln(os.Stdout, "first")
		_ = os.Stdout.Sync()
		time.Sleep(250 * time.Millisecond)
		fmt.Fprintln(os.Stdout, "second")
		os.Exit(0)
	case "wait":
		fmt.Printf("%d\n", os.Getpid())
		for {
			time.Sleep(time.Hour)
		}
	case "tree":
		child := exec.Command(os.Args[0], "-test.run=TestIsolationHelper", "--", "wait")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Printf("%d\n", os.Getpid())
		_ = child.Wait()
		os.Exit(0)
	case "leave-child":
		child := exec.Command(os.Args[0], "-test.run=TestIsolationHelper", "--", "wait")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Printf("%d\n", child.Process.Pid)
		os.Exit(0)
	case "write-pid":
		if len(marker) != 2 {
			os.Exit(2)
		}
		if err := os.WriteFile(marker[1], []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(2)
		}
		for {
			time.Sleep(time.Hour)
		}
	case "crash-parent":
		if len(marker) != 3 {
			os.Exit(2)
		}
		runner := testRunner()
		go func() {
			_, _ = runner.Run(context.Background(), helperRequest(marker[1], "write-pid", marker[2]))
		}()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(marker[2]); err == nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(3)
	}
}

func TestRunnerBoundsOutput(t *testing.T) {
	t.Parallel()
	result, err := testRunner().Run(t.Context(), Request{
		Executable: os.Args[0], Arguments: []string{"-test.run=TestIsolationHelper", "--", "output"},
		Directory: t.TempDir(), Limits: Limits{WallTime: 5 * time.Second, TerminateGrace: 50 * time.Millisecond, MaxStdoutBytes: 17, MaxStderrBytes: 11},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || len(result.Stdout) != 17 || len(result.Stderr) != 11 || !result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("bounded result = %#v", result)
	}
}

func TestRunnerStreamsBoundedOutputBeforeExit(t *testing.T) {
	observed := make(chan string, 4)
	finished := make(chan Result, 1)
	request := helperRequest(t.TempDir(), "stream")
	go func() {
		result, _ := testRunner().RunStreaming(t.Context(), request, func(chunk []byte) {
			observed <- string(chunk)
		})
		finished <- result
	}()
	select {
	case chunk := <-observed:
		if !strings.Contains(chunk, "first") {
			t.Fatalf("first streamed chunk = %q", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("stdout was not observed while the process was active")
	}
	select {
	case result := <-finished:
		t.Fatalf("process exited before streaming assertion: %#v", result)
	default:
	}
	if result := <-finished; !result.Successful() || !strings.Contains(string(result.Stdout), "second") {
		t.Fatalf("streamed result = %#v", result)
	}
}

func TestRunnerNeverStreamsPastOutputLimit(t *testing.T) {
	request := helperRequest(t.TempDir(), "output")
	request.Limits.MaxStdoutBytes = 17
	var observed bytes.Buffer
	result, err := testRunner().RunStreaming(t.Context(), request, func(chunk []byte) {
		_, _ = observed.Write(chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Len() != 17 || len(result.Stdout) != 17 || !result.StdoutTruncated {
		t.Fatalf("observed=%d result=%#v, want exactly 17 streamed bytes and truncation", observed.Len(), result)
	}
}

func TestRunnerCancellationKillsProcessGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(150*time.Millisecond, cancel)
	result, err := testRunner().Run(ctx, helperRequest(t.TempDir(), "tree"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Canceled {
		t.Fatalf("result = %#v, want canceled", result)
	}
	for _, line := range strings.Fields(string(result.Stdout)) {
		pid, parseErr := strconv.Atoi(line)
		if parseErr != nil {
			continue
		}
		waitForProcessExit(t, pid)
	}
}

func TestRunnerWallTimeout(t *testing.T) {
	t.Parallel()
	request := helperRequest(t.TempDir(), "wait")
	request.Limits.WallTime = 75 * time.Millisecond
	result, err := testRunner().Run(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || result.ExitCode != supervisorTimeoutExit {
		t.Fatalf("result = %#v, want timeout", result)
	}
}

func TestRunnerCleansBackgroundGroupBeforeReturning(t *testing.T) {
	t.Parallel()
	request := helperRequest(t.TempDir(), "leave-child")
	result, err := testRunner().Run(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("result = %#v", result)
	}
	fields := strings.Fields(string(result.Stdout))
	if len(fields) == 0 {
		t.Fatalf("child pid output = %q", result.Stdout)
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("child pid output = %q: %v", result.Stdout, err)
	}
	waitForProcessExit(t, pid)
}

func TestSupervisorCleansChildAfterParentCrash(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	pidFile := filepath.Join(directory, "child.pid")
	parent := exec.Command(os.Args[0], "-test.run=TestIsolationHelper", "--", "crash-parent", directory, pidFile)
	if output, err := parent.CombinedOutput(); err != nil {
		t.Fatalf("crash parent: %v: %s", err, output)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	waitForProcessExit(t, pid)
}

func testRunner() Runner {
	return Runner{SupervisorExecutable: os.Args[0], PrefixArguments: []string{"-test.run=TestIsolationHelper", "--"}}
}

func helperRequest(directory string, arguments ...string) Request {
	return Request{
		Executable: os.Args[0], Arguments: append([]string{"-test.run=TestIsolationHelper", "--"}, arguments...),
		Directory: directory,
		Limits:    Limits{WallTime: 5 * time.Second, TerminateGrace: 50 * time.Millisecond, MaxStdoutBytes: 4096, MaxStderrBytes: 4096},
	}
}

func argumentAfterDoubleDash(arguments []string) []string {
	for index, argument := range arguments {
		if argument == "--" && index+1 < len(arguments) {
			return arguments[index+1:]
		}
	}
	return nil
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d survived cleanup", pid)
}
