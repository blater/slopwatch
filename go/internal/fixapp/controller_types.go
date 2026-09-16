package fixapp

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/jobstore"
	"github.com/blater/slopwatch/internal/publisher"
)

type Manager struct {
	*controller
}

// controller owns the serialized job state machine and all of the resources
// used by its workers. Manager remains the public request facade; controller
// is the sole owner of scheduler mutation and lifecycle channels.
type controller struct {
	controllerServices
	controllerRuntime
	publication publicationOwner
	persistence persistenceOwner
	logging     loggingOwner
	workers     workerOwner
	recovery    recoveryOwner
	state       *controllerState
}

type controllerServices struct {
	deps    Dependencies
	options Options
	initial []jobstore.Record
}

type controllerRuntime struct {
	requests chan any
	events   chan agentUpdate
	results  chan workerResult
	done     chan struct{}
	ready    chan struct{}
	notifyMu sync.Mutex
	notify   chan struct{}
	closed   atomic.Bool
}

type jobRecord struct {
	input            FixInput
	presentation     fix.JobPresentation
	attempt          fix.AttemptID
	nextAttemptNotes string
	candidate        *fix.CandidateIdentity
	cancel           context.CancelFunc
	logs             []LogEntry
	commands         map[fix.CommandID]CommandReceipt
	actors           map[string]bool
	diffHash         string
	diffPaths        map[fix.RepoPath]bool
	baseScope        fix.ScopeState
	delivery         delivery.Result
	published        publisher.Result
	canceled         bool
	publicationStep  publicationStep
	resultLogged     bool
	agentReferences  []string
	runLock          jobstore.Lock
	runsHere         bool
	runsElsewhere    bool
	storedAt         time.Time
}

type controllerState struct {
	jobs             map[fix.JobID]*jobRecord
	order            []fix.JobID
	reservations     map[string]fix.JobID
	agentsRunning    int
	verifiersRunning int
	otherRunning     int
	shuttingDown     bool
	shutdownWaiters  []chan error
}

type runCall struct {
	ctx      context.Context
	input    FixInput
	response chan runResponse
}

type runResponse struct {
	id  fix.JobID
	err error
}

type commandCall struct {
	ctx      context.Context
	command  fix.JobCommand
	response chan commandResponse
}

type commandResponse struct {
	receipt CommandReceipt
	err     error
}

type candidateCall struct {
	id       fix.JobID
	response chan candidateResponse
}

type jobsCall struct {
	filter   JobFilter
	response chan JobListSnapshot
}

type transcriptCall struct {
	id       fix.JobID
	cursor   LogCursor
	limit    int
	response chan transcriptResponse
}

type transcriptResponse struct {
	page LogPage
	err  error
}

type candidateResponse struct {
	identity     fix.CandidateIdentity
	previewBytes int64
	previewLines int
	ok           bool
}

type shutdownCall struct{ response chan error }

type reconfigureCall struct {
	ctx      context.Context
	limits   RuntimeLimits
	response chan error
}

type workerKind uint8

const (
	workerCandidate workerKind = iota
	workerAgent
	workerVerifier
	workerDiscard
	workerCleanup
	workerPublish
)

type workerResult struct {
	kind           workerKind
	job            fix.JobID
	attempt        fix.AttemptID
	candidate      *fix.CandidateIdentity
	agent          agent.Result
	verify         fixanalysis.VerificationResult
	diff           candidate.DiffSnapshot
	delivery       delivery.Result
	deliveryTarget delivery.PreflightResult
	published      publisher.Result
	err            error
	inventoryErr   error
}

type agentUpdate struct {
	event   agent.Event
	prompt  *string
	barrier chan struct{}
	job     fix.JobID
	attempt fix.AttemptID
}

type publicationStep string

const (
	publicationCommit      publicationStep = "commit"
	publicationLocalRef    publicationStep = "local_ref"
	publicationRemoteRef   publicationStep = "remote_ref"
	publicationPullRequest publicationStep = "pull_request"
	publicationReconcile   publicationStep = "reconcile"
	publicationPRReconcile publicationStep = "pull_request_reconcile"
)
