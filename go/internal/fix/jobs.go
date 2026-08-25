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
	PhaseAwaitingReview  Phase = "awaiting_review"
	PhasePublishing      Phase = "publishing"
	PhaseReconciling     Phase = "reconciling"
	PhaseDiscarding      Phase = "discarding"
	PhaseCanceling       Phase = "canceling"
	PhaseAwaitingAction  Phase = "awaiting_action"
	PhaseCompleted       Phase = "completed"
	PhaseArchived        Phase = "archived"
	PhaseDiscarded       Phase = "discarded"
)

type Compliance string

const (
	ComplianceUnknown      Compliance = "unknown"
	ComplianceCompliant    Compliance = "compliant"
	ComplianceNoncompliant Compliance = "noncompliant"
)

type ScopeState string

const (
	ScopeUnknown    ScopeState = "unknown"
	ScopeClean      ScopeState = "clean"
	ScopeViolated   ScopeState = "violated"
	ScopeConflicted ScopeState = "conflicted"
)

type DeliveryState string
type DeliveryMode string

const (
	DeliveryModeCandidate   DeliveryMode = "candidate"
	DeliveryModeBranch      DeliveryMode = "branch"
	DeliveryModePullRequest DeliveryMode = "pull-request"
)

func (mode DeliveryMode) Valid() bool {
	switch mode {
	case DeliveryModeCandidate, DeliveryModeBranch, DeliveryModePullRequest:
		return true
	default:
		return false
	}
}

const (
	DeliveryNone        DeliveryState = "none"
	DeliveryCandidate   DeliveryState = "candidate"
	DeliveryCommitted   DeliveryState = "committed"
	DeliveryPushed      DeliveryState = "pushed"
	DeliveryPullRequest DeliveryState = "pull_request"
	DeliveryAmbiguous   DeliveryState = "ambiguous"
)

type Attention string

const (
	AttentionNone     Attention = "none"
	AttentionInfo     Attention = "information"
	AttentionRequired Attention = "action_required"
	AttentionBlocking Attention = "blocking"
)

type JobAction string

const (
	ActionCancel              JobAction = "cancel"
	ActionRetry               JobAction = "retry"
	ActionResume              JobAction = "resume"
	ActionPublish             JobAction = "publish"
	ActionKeep                JobAction = "keep"
	ActionArchive             JobAction = "archive"
	ActionDiscard             JobAction = "discard"
	ActionAcknowledgeConflict JobAction = "acknowledge_conflict"
	ActionCleanup             JobAction = "cleanup"
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
	ConflictCount   int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	FinishedAt      time.Time
	AllowedActions  []JobAction
	Compliance      Compliance
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
