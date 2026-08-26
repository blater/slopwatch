// Package agent defines the deep, provider-neutral runtime protocol used by
// fix orchestration. Provider adapters translate this protocol; they do not
// own scoring, Git delivery, preferences, or job lifecycle.
package agent

import (
	"context"

	"github.com/blater/slopwatch/internal/fix"
)

type RuntimeKind string
type ProfileID string
type ModelID string
type EffortID string
type DelegationMode string

const DelegationSingle DelegationMode = "single"

type Option[T ~string] struct {
	ID          T
	Label       string
	Description string
	Default     bool
}

type WriteConfinement uint8

const (
	AdvisoryOnly WriteConfinement = iota
	CandidateTreeEnforced
	CandidateTreeAndGitMetadataProtected
)

type RuntimeIsolation struct {
	Writes                      WriteConfinement
	SensitiveReadsDenied        bool
	TransportAuthIsolated       bool
	CrashContainment            bool
	ProviderManagedCancellation bool
}

func (value RuntimeIsolation) EligibleForMutation() bool {
	hostManaged := value.Writes == CandidateTreeAndGitMetadataProtected &&
		value.SensitiveReadsDenied && value.CrashContainment
	providerManaged := value.Writes >= CandidateTreeEnforced && value.ProviderManagedCancellation
	return providerManaged || (value.TransportAuthIsolated && hostManaged)
}

type NetworkCapability struct {
	TransportRequired bool
	ToolNetwork       bool
	ToolDomains       []string
}

type ProgressCapability string

const (
	ProgressNone       ProgressCapability = "none"
	ProgressText       ProgressCapability = "text"
	ProgressStructured ProgressCapability = "structured"
)

type Capabilities struct {
	Models     []Option[ModelID]
	Efforts    []Option[EffortID]
	Delegation []Option[DelegationMode]
	Resume     bool
	Progress   ProgressCapability
	Network    NetworkCapability
	Isolation  RuntimeIsolation
}

type Profile struct {
	ID                ProfileID
	Label             string
	Runtime           RuntimeKind
	Executable        string
	RuntimeProfile    string
	AuthenticationRef string
	Options           map[string]string
	Fingerprint       string
}

type ProfileField struct {
	Key             string
	OptionKey       string
	Label           string
	Description     string
	Kind            ProfileFieldKind
	Required        bool
	Default         string
	Choices         []string
	Pattern         string
	PreferencesOnly bool
}

type ProfileFieldKind string

const (
	ProfileFieldExecutable    ProfileFieldKind = "executable"
	ProfileFieldAuthReference ProfileFieldKind = "auth-reference"
	ProfileFieldText          ProfileFieldKind = "text"
	ProfileFieldPathList      ProfileFieldKind = "path-list"
	ProfileFieldChoice        ProfileFieldKind = "choice"
)

type ProfileDescriptor struct {
	Runtime                RuntimeKind
	Label                  string
	ConnectionInstructions string
	DocumentationURL       string
	Fields                 []ProfileField
}

type ProfileCatalog interface {
	Kinds() []RuntimeKind
	Descriptor(RuntimeKind) (ProfileDescriptor, error)
	ValidateProfile(Profile) error
}
type Prober interface {
	Probe(context.Context, Profile) ProbeResult
}

type ProbeState string

const (
	ProbeReady           ProbeState = "ready"
	ProbeUnavailable     ProbeState = "unavailable"
	ProbeUnauthenticated ProbeState = "unauthenticated"
	ProbeIncompatible    ProbeState = "incompatible"
	ProbeDegraded        ProbeState = "degraded"
)

type ProbeResult struct {
	Runtime        RuntimeKind
	Version        string
	State          ProbeState
	Diagnostic     string
	Authentication Authentication
	Capabilities   Capabilities
}

// Authentication is sanitized provider account metadata. Adapters must never
// place credentials or refresh material here; it exists so the UI can state
// which deliberately selected auth/billing route is active.
type Authentication struct {
	Method string
	Label  string
}

type WritePolicy struct {
	Allowed []fix.RepoPath
	Scope   string
}

type Limits struct {
	MaxOutputBytes int64
	MaxEvents      int
	MaxActors      int
}

type InstructionDocument struct {
	Version          string
	Envelope         string
	Objective        string
	NextAttemptNotes string
}

func (document InstructionDocument) EffectiveBody() string {
	result := document.Envelope + "\n\n" + document.Objective
	if document.NextAttemptNotes != "" {
		result += "\n\nMeasurements from the previous attempt:\n" + document.NextAttemptNotes
	}
	return result
}

type ValidationContract struct {
	PlanID   string
	Required bool
}

type RemediationTask struct {
	Targets      []fix.TargetSnapshot
	Goal         fix.ScoringGoal
	Evidence     []fix.MetricEvidence
	Instructions InstructionDocument
	Validation   ValidationContract
}

type Request struct {
	JobID      fix.JobID
	AttemptID  fix.AttemptID
	Workspace  fix.CandidateIdentity
	Task       RemediationTask
	Model      ModelID
	Effort     EffortID
	Delegation DelegationMode
	Write      WritePolicy
	Limits     Limits
	Resume     ResumeToken
}

type ResumeToken struct {
	Reference          string
	Runtime            RuntimeKind
	ProfileFingerprint string
	Workspace          fix.CandidateIdentity
}

type Strategy interface {
	ProfileDescriptor() ProfileDescriptor
	ValidateProfile(Profile) error
	Probe(context.Context, Profile) ProbeResult
	Execute(context.Context, Profile, Request, EventSink) Result
}
