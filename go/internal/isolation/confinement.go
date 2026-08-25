package isolation

import (
	"context"
	"errors"
	"fmt"
	"runtime"
)

// ConfinementCapability is composition-time evidence about a candidate
// executor. It is intentionally separate from per-launch Conformance: a
// backend must first own escaped descendants, then every exact launch must
// still prove its file, network, Git and credential boundaries.
type ConfinementCapability struct {
	Available        bool
	CrashContainment bool
	Backend          string
	Diagnostic       string
}

type CandidatePolicy struct {
	CandidateRoot  string
	GitCommonDir   string
	SensitiveRoots []string
}

// CandidateConfinement is the shared platform boundary used by coding-agent
// adapters and trusted validation. A future container backend belongs here;
// providers must not guess platform capabilities themselves.
type CandidateConfinement interface {
	Capability(context.Context) ConfinementCapability
	RunCandidate(context.Context, CandidatePolicy, Request) (Result, Conformance, error)
}

type UnsupportedConfinement struct{ Reason string }

func (value UnsupportedConfinement) Capability(context.Context) ConfinementCapability {
	reason := value.Reason
	if reason == "" {
		reason = "no candidate confinement backend is configured"
	}
	return ConfinementCapability{Diagnostic: reason}
}

func (value UnsupportedConfinement) RunCandidate(context.Context, CandidatePolicy, Request) (Result, Conformance, error) {
	capability := value.Capability(context.Background())
	return Result{}, Conformance{Diagnostic: capability.Diagnostic}, errors.New(capability.Diagnostic)
}

type ConfinementOptions struct {
	// ContainerImage is reserved for an immutable, installation-owned image.
	// User/repository text must never be promoted to an executable backend.
	ContainerImage string
}

// SelectCandidateConfinement is the single production capability selector.
// Native process groups are lifecycle helpers, not crash containment. Until a
// configured immutable container backend is implemented and empirically
// proven, every platform remains unavailable rather than being guessed safe.
func SelectCandidateConfinement(_ Executor, options ConfinementOptions) CandidateConfinement {
	if options.ContainerImage != "" {
		return UnsupportedConfinement{Reason: "container candidate confinement is configured but no verified container backend is installed"}
	}
	return UnsupportedConfinement{Reason: fmt.Sprintf("candidate confinement is unavailable on %s: a process group cannot own descendants that create a new session", runtime.GOOS)}
}
