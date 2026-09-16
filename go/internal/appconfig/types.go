// Package appconfig is the typed preferences boundary used by application
// services and the TUI. Consumers never receive a file path or TOML document.
package appconfig

import (
	"context"
	"errors"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

type Revision uint64
type Scope string

const (
	ScopeUser       Scope = "user"
	ScopeRepository Scope = "repository"
)

type Origin string

const (
	OriginBuiltIn    Origin = "built_in"
	OriginUser       Origin = "user"
	OriginRepository Origin = "repository"
	OriginCLI        Origin = "cli"
	OriginSession    Origin = "session"
)

var ErrRevisionConflict = errors.New("preferences revision conflict")

type SessionOverrides struct {
	TargetScore *float64
	Profile     *agent.ProfileID
	Model       *agent.ModelID
	Effort      *agent.EffortID
	TrendWindow *time.Duration
}

type FixDefaults struct {
	TargetScore    float64
	Focus          []fix.MetricID
	ChangeScope    string
	Profile        agent.ProfileID
	Model          agent.ModelID
	Effort         agent.EffortID
	PromptTemplate string
}

type Concurrency struct {
	MaxAgents                int
	MaxVerifiers             int
	MaxActorsPerJob          int
	MaxCandidatePreviewBytes int64
	MaxCandidatePreviewLines int
}

type Delivery struct {
	DefaultPlan              fix.DeliveryPlan
	Remote                   string
	BaseBranch               string
	BranchTemplate           string
	Publisher                string
	DraftPullRequests        bool
	CommandOutputBytes       int64
	CommitTitleTemplate      string
	CommitBodyTemplate       string
	PullRequestTitleTemplate string
	PullRequestBodyTemplate  string
}

type Resolved struct {
	SchemaVersion int
	Revision      Revision
	Origins       map[string]Origin
	Fix           FixDefaults
	Concurrency   Concurrency
	Profiles      []agent.Profile
	Delivery      Delivery
	TrendWindow   time.Duration
}

type Patch struct {
	Fix         *FixDefaults
	Concurrency *Concurrency
	Profiles    *[]agent.Profile
	Delivery    *Delivery
	TrendWindow *time.Duration
}

type Saved struct {
	Revision Revision
	Resolved Resolved
}

type Editable struct {
	Resolved    Resolved
	Diagnostics []string
}
type Editor interface {
	LoadEditable(context.Context, fix.WorkspaceIdentity) (Editable, error)
}

type Resolver interface {
	Resolve(context.Context, fix.WorkspaceIdentity, SessionOverrides) (Resolved, error)
}

type Store interface {
	Save(context.Context, fix.WorkspaceIdentity, Scope, Patch, Revision) (Saved, error)
}
