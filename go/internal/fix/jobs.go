package fix

import "time"

type Phase string

const (
	PhaseAdmitted        Phase = "admitted"
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
type DeliveryMode string

const (
	DeliveryModeBranch      DeliveryMode = "branch"
	DeliveryModePullRequest DeliveryMode = "pull-request"
)

func (mode DeliveryMode) Valid() bool {
	switch mode {
	case DeliveryModeBranch, DeliveryModePullRequest:
		return true
	default:
		return false
	}
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
	// Metrics is retained for journal compatibility. New code projects
	// baseline and verified values separately.
	Metrics        []MetricValue
	Changed        bool
	ScopeViolation bool
	Verification   string
}

type JobPresentation struct {
	ID              JobID
	Revision        uint64
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
	Validation      ValidationState
	Scope           ScopeState
	Delivery        DeliveryState
	DeliveryMode    DeliveryMode
	BranchName      string
	DiffFingerprint string
	Issue           *JobIssue
}

type JobCommand struct {
	RequestID        CommandID
	JobID            JobID
	ExpectedRevision uint64
	Action           JobAction
	DiffHash         string
	Value            string
}
