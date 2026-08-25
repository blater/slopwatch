package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/isolation"
)

func TestCommandClientHelper(t *testing.T) {
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) && os.Args[index+1] == "hold-stdin" {
			_, _ = io.Copy(io.Discard, os.Stdin)
			fmt.Fprint(os.Stdout, "control-eof")
			os.Exit(0)
		}
	}
}

func TestCommandClientAttachKeepsControlOpenUntilCancellation(t *testing.T) {
	client, err := NewCommandClient(os.Args[0], "unix:///private/tmp/fake-docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	var streamed strings.Builder
	result, err := client.Run(ctx, Command{Arguments: []string{"-test.run=TestCommandClientHelper", "--", "hold-stdin"}, Attach: true,
		Limits: isolation.Limits{WallTime: time.Second, TerminateGrace: time.Second, MaxStdoutBytes: 1024, MaxStderrBytes: 1024}}, func(data []byte) { streamed.Write(data) })
	if err != nil || !result.Canceled || string(result.Stdout) != "control-eof" || streamed.String() != "control-eof" {
		t.Fatalf("attach result=%+v stream=%q err=%v", result, streamed.String(), err)
	}
}

func TestCommandClientRejectsMissingLimitsBeforeStartingProcess(t *testing.T) {
	client, err := NewCommandClient(os.Args[0], "unix:///private/tmp/fake-docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	for _, limits := range []isolation.Limits{
		{},
		{TerminateGrace: time.Second, MaxStdoutBytes: 1, MaxStderrBytes: 1},
		{WallTime: time.Second, MaxStdoutBytes: 1, MaxStderrBytes: 1},
		{WallTime: time.Second, TerminateGrace: time.Second, MaxStderrBytes: 1},
		{WallTime: time.Second, TerminateGrace: time.Second, MaxStdoutBytes: 1},
	} {
		result, err := client.Run(t.Context(), Command{Arguments: []string{"-test.run=TestCommandClientHelper"}, Limits: limits}, nil)
		if err == nil || !strings.Contains(err.Error(), "requires explicit positive") {
			t.Fatalf("Run(%+v) result=%+v error=%v", limits, result, err)
		}
	}
}

func TestCommandClientRejectsRemoteDaemon(t *testing.T) {
	for _, host := range []string{
		"tcp://example.test:2375", "unix://relative.sock", "unix://host/private/docker.sock",
		"unix:///private/tmp/../docker.sock", "unix:///private/tmp/docker.sock?unsafe=1",
	} {
		if _, err := NewCommandClient(os.Args[0], host); err == nil {
			t.Fatalf("unsafe Docker daemon %q was accepted", host)
		}
	}
}
