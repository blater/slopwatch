// Package isolation runs supervised child processes with bounded output and
// per-process cancellation.
package isolation

import (
	"context"
	"errors"
	"time"
)

const SupervisorArgument = "__slopwatch_agent_supervisor_v1"

var (
	ErrInvalidRequest     = errors.New("invalid isolated process request")
	ErrSupervisorProtocol = errors.New("invalid supervisor protocol")
)

type Limits struct {
	WallTime       time.Duration
	TerminateGrace time.Duration
	MaxStdoutBytes int64
	MaxStderrBytes int64
}

type Request struct {
	Executable  string
	Arguments   []string
	Directory   string
	Environment []string
	Stdin       []byte
	Limits      Limits
}

type Result struct {
	ExitCode        int
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
	Canceled        bool
	TimedOut        bool
}

func (result Result) Successful() bool {
	return result.ExitCode == 0 && !result.Canceled && !result.TimedOut
}

// Runner re-executes a trusted binary in supervisor mode. PrefixArguments is
// intended for Go test helper processes; production composition leaves it nil.
type Runner struct {
	SupervisorExecutable string
	PrefixArguments      []string
}

type Executor interface {
	Run(context.Context, Request) (Result, error)
}

// StreamingExecutor reports bounded stdout chunks while retaining the same
// final Result contract. Observers must return quickly; chunks are copied and
// are never delivered beyond the configured stdout byte limit.
type StreamingExecutor interface {
	Executor
	RunStreaming(context.Context, Request, func([]byte)) (Result, error)
}
