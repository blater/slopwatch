package fixapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/publisher"
)

type publicationOwner struct {
	candidates candidate.Service
	delivery   delivery.SagaService
	preflight  delivery.PreflightService
	publisher  publisher.Service
	results    chan<- workerResult
}

func publicationRequests(input FixInput, job fix.JobID, identity fix.CandidateIdentity, diffHash string, paths []fix.RepoPath, delivered delivery.Result) (delivery.Request, publisher.Request) {
	commitTitle := renderPublicationTemplate(input.Preferences.Delivery.CommitTitleTemplate, "Refactor {targets} with Slopwatch", input, job)
	commitBody := renderPublicationTemplate(input.Preferences.Delivery.CommitBodyTemplate, "Automated remediation for {goal}.", input, job)
	prTitle := renderPublicationTemplate(input.Preferences.Delivery.PullRequestTitleTemplate, commitTitle, input, job)
	prBody := renderPublicationTemplate(input.Preferences.Delivery.PullRequestBodyTemplate, commitBody, input, job)
	request := delivery.Request{Job: job, Candidate: identity, DiffHash: diffHash, Plan: input.DeliveryPlan, Paths: append([]fix.RepoPath(nil), paths...), Branch: input.BranchName,
		Remote: input.Preferences.Delivery.Remote, CommitTitle: commitTitle, CommitBody: commitBody,
		ExpectedRemoteHost: input.DeliveryTarget.RemoteHost, HostRepository: input.DeliveryTarget.HostRepository,
		ExpectedRemoteIdentity: input.DeliveryTarget.RemoteIdentity, CommandOutputBytes: input.Preferences.Delivery.CommandOutputBytes}
	pullRequest := publisher.Request{Job: job, Repository: identity.Repository, Candidate: identity, HostRepository: delivered.Repository,
		Remote: input.Preferences.Delivery.Remote, BaseBranch: input.Preferences.Delivery.BaseBranch, HeadBranch: input.BranchName,
		Commit: delivered.Commit, Title: prTitle, Body: prBody, Draft: input.Preferences.Delivery.DraftPullRequests,
		CommandOutputBytes: input.Preferences.Delivery.CommandOutputBytes}
	return request, pullRequest
}

func renderPublicationTemplate(template, fallback string, input FixInput, job fix.JobID) string {
	if strings.TrimSpace(template) == "" {
		template = fallback
	}
	return strings.NewReplacer("{targets}", targetLabel(input.Targets), "{goal}", goalLabel(input), "{branch}", input.BranchName, "{job}", string(job)).Replace(template)
}

func (owner *publicationOwner) runPublicationStep(ctx context.Context, step publicationStep, input FixInput, job fix.JobID, attempt fix.AttemptID,
	identity fix.CandidateIdentity, diffHash string, paths []fix.RepoPath, delivered delivery.Result, published publisher.Result) {
	target := input.DeliveryTarget
	preflightErr := error(nil)
	if step == publicationCommit {
		target, preflightErr = owner.checkPublicationCommit(ctx, input, identity, diffHash, target)
		if preflightErr == nil {
			input.DeliveryTarget = target
		}
	}
	request, pullRequest := publicationRequests(input, job, identity, diffHash, paths, delivered)
	delivered, published, err := owner.executePublicationStep(ctx, step, request, pullRequest, delivered, published, preflightErr)
	owner.results <- workerResult{kind: workerPublish, job: job, attempt: attempt, delivery: delivered, deliveryTarget: target, published: published, err: err}
}

func (owner *publicationOwner) checkPublicationCommit(ctx context.Context, input FixInput, identity fix.CandidateIdentity, diffHash string, previous delivery.PreflightResult) (delivery.PreflightResult, error) {
	latest, err := owner.candidates.Diff(ctx, identity)
	if err != nil {
		return previous, fmt.Errorf("check files before commit: %w", err)
	}
	if latest.Fingerprint != diffHash {
		return previous, errors.New("files changed after agent completion")
	}
	current, err := owner.preflightDelivery(ctx, input.Workspace, input.DeliveryPlan, input.Preferences.Delivery, input.BranchName, true)
	if err != nil {
		return previous, err
	}
	if previous != (delivery.PreflightResult{}) && current != previous {
		return previous, errors.New("delivery target changed since publication began")
	}
	return current, nil
}

func (owner *publicationOwner) executePublicationStep(ctx context.Context, step publicationStep, request delivery.Request, pullRequest publisher.Request,
	delivered delivery.Result, published publisher.Result, previousErr error) (delivery.Result, publisher.Result, error) {
	if previousErr != nil {
		return delivered, published, previousErr
	}
	switch step {
	case publicationCommit:
		result, err := owner.delivery.CreateCommit(ctx, request)
		return result, published, err
	case publicationLocalRef:
		result, err := owner.delivery.CreateLocalRef(ctx, request, delivered)
		return result, published, err
	case publicationRemoteRef:
		result, err := owner.delivery.CreateRemoteRef(ctx, request, delivered)
		return result, published, err
	case publicationReconcile:
		result, err := owner.delivery.Reconcile(ctx, request, delivered)
		return result, published, err
	case publicationPRReconcile:
		if owner.publisher == nil {
			return delivered, published, errors.New("pull request publisher is unavailable")
		}
		pullRequest.Commit = delivered.Commit
		result, err := owner.publisher.Reconcile(ctx, pullRequest, published)
		return delivered, result, err
	case publicationPullRequest:
		if owner.publisher == nil {
			return delivered, published, errors.New("pull request publisher is unavailable")
		}
		pullRequest.Commit = delivered.Commit
		result, err := owner.publisher.Create(ctx, pullRequest)
		return delivered, result, err
	default:
		return delivered, published, errors.New("unknown publication step")
	}
}
