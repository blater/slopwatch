package fixapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/fixprompt"
)

type workerOwner struct {
	agents     *agent.Registry
	candidates candidate.Service
	analysis   fixanalysis.Service
	clock      func() time.Time
	events     chan<- agentUpdate
	results    chan<- workerResult
	done       <-chan struct{}
}

func (owner *workerOwner) runCandidatePrepare(ctx context.Context, input FixInput, job fix.JobID, attempt fix.AttemptID) {
	identity, err := owner.candidates.Prepare(ctx, candidate.PrepareRequest{Job: job, Workspace: input.Workspace,
		Mode: input.DeliveryPlan.Workspace, Targets: input.Targets, AllowedScope: input.ChangeScope, AllowedPaths: input.AllowedPaths,
		CommandOutputBytes: input.Preferences.Delivery.CommandOutputBytes})
	if err != nil {
		err = fmt.Errorf("prepare candidate: %w", err)
	}
	owner.results <- workerResult{kind: workerCandidate, job: job, attempt: attempt, candidate: candidatePointer(identity, err), err: err}
}

func candidatePointer(identity fix.CandidateIdentity, err error) *fix.CandidateIdentity {
	if err != nil {
		return nil
	}
	return &identity
}

func (owner *workerOwner) runAgent(ctx context.Context, input FixInput, nextAttemptNotes string, job fix.JobID, attempt fix.AttemptID, identity fix.CandidateIdentity) {
	select {
	case owner.events <- agentUpdate{event: agent.Event{JobID: job, AttemptID: attempt, At: owner.clock(), Kind: agent.EventActivity, Summary: "Candidate ready; starting agent"}}:
	case <-owner.done:
		return
	}
	strategy, err := owner.agents.Strategy(input.Profile.Runtime)
	if err != nil {
		owner.finishAgentWorker(job, attempt, workerResult{kind: workerAgent, job: job, attempt: attempt, candidate: &identity, err: err})
		return
	}
	instructions := input.Instructions
	instructions.NextAttemptNotes = nextAttemptNotes
	manifest, err := prepareTargetManifest(identity, input.Targets)
	if err != nil {
		owner.finishAgentWorker(job, attempt, workerResult{kind: workerAgent, job: job, attempt: attempt, candidate: &identity, err: fmt.Errorf("prepare target manifest: %w", err)})
		return
	}
	request := agent.Request{JobID: job, AttemptID: attempt, Workspace: identity, Model: input.Model, Effort: input.Effort,
		Task:   agent.RemediationTask{Targets: cloneContract(input.Baseline.Contract).Targets, Goal: input.Baseline.Contract.Goal, Instructions: instructions, Manifest: manifest},
		Write:  agent.WritePolicy{Allowed: append([]fix.RepoPath(nil), input.AllowedPaths...), Scope: input.ChangeScope},
		Limits: agent.Limits{MaxActors: input.Preferences.Concurrency.MaxActorsPerJob}}
	prompt := request.Task.EffectivePrompt()
	select {
	case owner.events <- agentUpdate{job: job, attempt: attempt, prompt: &prompt}:
	case <-owner.done:
		return
	}
	result := strategy.Execute(ctx, input.Profile, request, agent.EventSinkFunc(func(event agent.Event) error {
		if event.JobID != job || event.AttemptID != attempt {
			return errors.New("agent event identity mismatch")
		}
		select {
		case owner.events <- agentUpdate{event: event}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-owner.done:
			return ErrClosed
		}
	}))
	diff, inventoryErr := owner.candidates.Diff(ctx, identity)
	owner.finishAgentWorker(job, attempt, workerResult{kind: workerAgent, job: job, attempt: attempt, candidate: &identity,
		agent: result, diff: diff, inventoryErr: inventoryErr})
}

func prepareTargetManifest(identity fix.CandidateIdentity, targets []fix.RepoPath) (*agent.TargetManifest, error) {
	if !fixprompt.RequiresTargetManifest(targets) {
		return nil, nil
	}
	if identity.StagingRoot == "" || !filepath.IsAbs(identity.StagingRoot) {
		return nil, errors.New("candidate staging is unavailable")
	}
	directory := filepath.Join(identity.StagingRoot, "agent-input")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, err
	}
	paths := append([]fix.RepoPath(nil), targets...)
	sort.Slice(paths, func(i, j int) bool { return paths[i] < paths[j] })
	lines := make([]string, len(paths))
	for index, path := range paths {
		lines[index] = path.String()
	}
	manifestPath := filepath.Join(directory, "targets.txt")
	if err := os.WriteFile(manifestPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return nil, err
	}
	if err := os.Chmod(manifestPath, 0o600); err != nil {
		return nil, err
	}
	return &agent.TargetManifest{Path: manifestPath, Count: len(paths)}, nil
}

func (owner *workerOwner) finishAgentWorker(job fix.JobID, attempt fix.AttemptID, result workerResult) {
	barrier := make(chan struct{})
	select {
	case owner.events <- agentUpdate{barrier: barrier, job: job, attempt: attempt}:
		select {
		case <-barrier:
		case <-owner.done:
			return
		}
	case <-owner.done:
		return
	}
	select {
	case owner.results <- result:
	case <-owner.done:
	}
}

func (owner *workerOwner) runVerifier(ctx context.Context, input FixInput, job fix.JobID, attempt fix.AttemptID, identity fix.CandidateIdentity) {
	diff, err := owner.candidates.Diff(ctx, identity)
	if err != nil {
		owner.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff, err: fmt.Errorf("inventory candidate diff: %w", err)}
		return
	}
	// Compatibility for jobs saved in the old verifier phase: retain their
	// changed-file listing, without running an analysis acceptance gate.
	owner.results <- workerResult{kind: workerVerifier, job: job, attempt: attempt, diff: diff}
}

func targetLabel(targets []fix.RepoPath) string {
	switch len(targets) {
	case 0:
		return "candidate"
	case 1:
		return targets[0].String()
	default:
		return fmt.Sprintf("%d files", len(targets))
	}
}
