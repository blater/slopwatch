package fix

import "time"

type Phase string

const (
	PhaseQueued          Phase = "queued"
	PhasePreflight       Phase = "preflight"
	PhasePreparing       Phase = "preparing"
	PhaseRunning         Phase = "running"
	PhaseWaitingVerifier Phase = "waiting_verifier"
	PhaseVerifying       Phase = "verifying"
	PhaseFailed          Phase = "failed"
	PhasePublishing      Phase = "publishing"
	PhaseReconciling     Phase = "reconciling"
	PhaseDiscarding      Phase = "discarding"
	PhaseCanceling       Phase = "canceling"
	PhaseCompleted       Phase = "completed"
	PhaseCanceled        Phase = "canceled"
	PhaseDiscarded       Phase = "discarded"
)

type TargetStatus string

const (
	ScorePending TargetStatus = "score_pending"
	TargetMet    TargetStatus = "met"
	TargetNotMet TargetStatus = "not_met"
)

type ScopeState string

const (
	ScopeUnknown  ScopeState = "unknown"
	ScopeClean    ScopeState = "clean"
	ScopeViolated ScopeState = "violated"
)

type DeliveryState string
type WorkspaceMode string
type GitMode string
type PublishMode string

const (
	WorkspaceCurrent  WorkspaceMode = "current-files"
	WorkspaceWorktree WorkspaceMode = "worktree"

	GitLeaveUncommitted GitMode = "uncommitted"
	GitCommitCurrent    GitMode = "current-branch"
	GitCommitNewBranch  GitMode = "new-branch"

	PublishLocal       PublishMode = "local"
	PublishPush        PublishMode = "push"
	PublishPullRequest PublishMode = "pull-request"
)

func (mode WorkspaceMode) Valid() bool {
	switch mode {
	case WorkspaceCurrent, WorkspaceWorktree:
		return true
	default:
		return false
	}
}

func (mode GitMode) Valid() bool {
	switch mode {
	case GitLeaveUncommitted, GitCommitCurrent, GitCommitNewBranch:
		return true
	default:
		return false
	}
}

func (mode PublishMode) Valid() bool {
	switch mode {
	case PublishLocal, PublishPush, PublishPullRequest:
		return true
	default:
		return false
	}
}

type DeliveryPlan struct {
	Workspace WorkspaceMode
	Git       GitMode
	Publish   PublishMode
}

func (plan DeliveryPlan) Valid() bool {
	if !plan.Workspace.Valid() || !plan.Git.Valid() || !plan.Publish.Valid() {
		return false
	}
	return plan.Git != GitLeaveUncommitted || plan.Publish == PublishLocal
}

const (
	DeliveryNone        DeliveryState = "none"
	DeliveryCommitted   DeliveryState = "committed"
	DeliveryPushed      DeliveryState = "pushed"
	DeliveryPullRequest DeliveryState = "pull_request"
	DeliveryAmbiguous   DeliveryState = "ambiguous"
)

type Attention string

const (
	AttentionNone     Attention = "none"
	AttentionInfo     Attention = "information"
	AttentionError    Attention = "error"
	AttentionBlocking Attention = "blocking"
)

type JobAction string

const (
	ActionCancel JobAction = "cancel"
)

type JobIssue struct {
	Code    string
	Summary string
	Detail  string
}

type ActorPresentation struct {
	ID, ParentID, CurrentAction string
}

type UsagePresentation struct {
	InputTokens, CachedTokens, OutputTokens, ReasoningTokens int64
}

type FilePresentation struct {
	Path            RepoPath
	PreviousPath    RepoPath
	Classification  string
	ChangeStatus    string
	BaselineScore   float64
	VerifiedScore   *float64
	BaselineMetrics []MetricValue
	VerifiedMetrics []MetricValue
	// Metrics is retained for saved-state compatibility. New code projects
	// baseline and verified values separately.
	Metrics        []MetricValue
	Changed        bool
	ScopeViolation bool
	Verification   string
}

type JobPresentation struct {
	ID              JobID
	Phase           Phase
	Attention       Attention
	ProfileLabel    string
	ProfileID       string
	ModelLabel      string
	EffortLabel     string
	Goal            string
	Targets         []FilePresentation
	CurrentAction   string
	AttemptOrdinal  int
	ActorCount      int
	Actors          []ActorPresentation
	Usage           UsagePresentation
	UsageReported   bool
	WarningCount    int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	FinishedAt      time.Time
	AllowedActions  []JobAction
	TargetStatus    TargetStatus
	Scope           ScopeState
	Delivery        DeliveryState
	DeliveryPlan    DeliveryPlan
	BranchName      string
	WorkspacePath   string
	DiffFingerprint string
	Issue           *JobIssue
}

type JobCommand struct {
	RequestID CommandID
	JobID     JobID
	Action    JobAction
}
