package agent

import "github.com/blater/slopwatch/internal/fix"

type ResultStatus string

const (
	ResultCompleted ResultStatus = "completed"
	ResultFailed    ResultStatus = "failed"
	ResultCanceled  ResultStatus = "canceled"
	ResultTimedOut  ResultStatus = "timed_out"
)

type FailureClass string

const (
	FailureNone                  FailureClass = ""
	FailureUnavailable           FailureClass = "unavailable"
	FailureIncompatible          FailureClass = "incompatible"
	FailureUnauthenticated       FailureClass = "unauthenticated"
	FailureInvalidProfile        FailureClass = "invalid_profile"
	FailureUnsupportedCapability FailureClass = "unsupported_capability"
	FailureLaunch                FailureClass = "launch"
	FailureProtocol              FailureClass = "protocol"
	FailureProvider              FailureClass = "provider"
	FailureCancellation          FailureClass = "cancellation"
	FailureTimeout               FailureClass = "timeout"
)

type Result struct {
	JobID            fix.JobID
	AttemptID        fix.AttemptID
	Status           ResultStatus
	Failure          FailureClass
	ExitCode         int
	Summary          string
	Diagnostic       string
	Usage            Usage
	SessionReference string
}
