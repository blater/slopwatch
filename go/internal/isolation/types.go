// Package isolation runs untrusted agent processes behind a self-reexec
// supervisor. Presence of the runner is not itself proof that a platform
// contains every descendant; mutation eligibility is reported separately by
// Conformance.
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

type Gate string

const (
	GateCandidateWrite       Gate = "candidate_write"
	GateOutsideWriteDenied   Gate = "outside_write_denied"
	GateGitMetadataDenied    Gate = "git_metadata_denied"
	GateSensitiveReadsDenied Gate = "sensitive_reads_denied"
	GateToolNetworkPolicy    Gate = "tool_network_policy"
	GateTransportAuth        Gate = "transport_auth"
	GateCrashContainment     Gate = "crash_containment"
)

// Conformance contains measured guarantees, never inferred capabilities.
type Conformance struct {
	CandidateWrite       bool
	OutsideWriteDenied   bool
	GitMetadataDenied    bool
	SensitiveReadsDenied bool
	ToolNetworkPolicy    bool
	TransportAuth        bool
	CrashContainment     bool
	Diagnostic           string
}

func (value Conformance) MutationEligible() bool {
	return value.CandidateWrite && value.OutsideWriteDenied &&
		value.GitMetadataDenied && value.SensitiveReadsDenied &&
		value.ToolNetworkPolicy && value.TransportAuth &&
		value.CrashContainment
}

func (value Conformance) FailedGates() []Gate {
	gates := make([]Gate, 0, 7)
	checks := []struct {
		gate Gate
		ok   bool
	}{
		{GateCandidateWrite, value.CandidateWrite},
		{GateOutsideWriteDenied, value.OutsideWriteDenied},
		{GateGitMetadataDenied, value.GitMetadataDenied},
		{GateSensitiveReadsDenied, value.SensitiveReadsDenied},
		{GateToolNetworkPolicy, value.ToolNetworkPolicy},
		{GateTransportAuth, value.TransportAuth},
		{GateCrashContainment, value.CrashContainment},
	}
	for _, check := range checks {
		if !check.ok {
			gates = append(gates, check.gate)
		}
	}
	return gates
}

type ConformanceRequest struct {
	Executable            string
	ProfileArguments      []string
	ProfileFingerprint    string
	CandidateRoot         string
	GitCommonDir          string
	OutsideRoot           string
	SensitiveRoots        []string
	TransportAuthVerified bool
}

type Checker interface {
	Check(context.Context, ConformanceRequest) Conformance
}

type CheckerFunc func(context.Context, ConformanceRequest) Conformance

func (function CheckerFunc) Check(ctx context.Context, request ConformanceRequest) Conformance {
	return function(ctx, request)
}

// DenyAllChecker is the safe default until a platform-specific executable
// conformance suite has established every required boundary.
type DenyAllChecker struct {
	Diagnostic string
}

func (checker DenyAllChecker) Check(context.Context, ConformanceRequest) Conformance {
	diagnostic := checker.Diagnostic
	if diagnostic == "" {
		diagnostic = "runtime confinement has not been proven on this platform"
	}
	return Conformance{Diagnostic: diagnostic}
}
