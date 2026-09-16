package fixapp

import (
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/fix"
)

type publicationDecision struct {
	step   publicationStep
	phase  fix.Phase
	action string
	finish bool
}

func (manager *controller) startNextPublication(record *jobRecord) {
	if manager.publication.delivery == nil || record.candidate == nil {
		manager.failRecord(record, "publication_unavailable", errors.New("publication service or candidate is unavailable"))
		return
	}
	decision := publicationDecisionFor(record)
	if decision.finish {
		if record.canceled {
			manager.finishCancellation(record)
			return
		}
		manager.finishPublication(record)
		return
	}
	record.presentation.Phase = decision.phase
	record.presentation.CurrentAction = decision.action
	manager.startPublicationWorker(record, decision.step)
}

func publicationDecisionFor(record *jobRecord) publicationDecision {
	if record.canceled {
		switch {
		case record.delivery.Ambiguous:
			return publicationDecision{publicationReconcile, fix.PhaseReconciling, "Checking canceled branch delivery", false}
		case record.published.Ambiguous:
			return publicationDecision{publicationPRReconcile, fix.PhaseReconciling, "Checking canceled pull request delivery", false}
		default:
			return publicationDecision{finish: true}
		}
	}
	switch {
	case record.delivery.Ambiguous:
		return publicationDecision{publicationReconcile, fix.PhaseReconciling, "Reconciling exact remote ref", false}
	case record.delivery.Commit == "":
		return publicationDecision{publicationCommit, fix.PhasePublishing, "Creating candidate commit", false}
	case record.delivery.LocalRef == "":
		return publicationDecision{publicationLocalRef, fix.PhasePublishing, "Creating absent local branch", false}
	case record.input.DeliveryPlan.Publish == fix.PublishLocal:
		return publicationDecision{finish: true}
	case !record.delivery.Pushed:
		return publicationDecision{publicationRemoteRef, fix.PhasePublishing, "Creating and verifying absent remote branch", false}
	case record.input.DeliveryPlan.Publish == fix.PublishPullRequest && record.published.Ambiguous:
		return publicationDecision{publicationPRReconcile, fix.PhaseReconciling, "Reconciling exact pull request identity", false}
	case record.input.DeliveryPlan.Publish == fix.PublishPullRequest && record.published.URL == "":
		return publicationDecision{publicationPullRequest, fix.PhasePublishing, "Creating or reconciling input pull request", false}
	default:
		return publicationDecision{finish: true}
	}
}

func (manager *controller) finishPublication(record *jobRecord) {
	state := manager.state
	if manager.publication.candidates == nil {
		record.presentation.Phase = fix.PhaseCompleted
		record.presentation.Attention = fix.AttentionNone
		record.presentation.CurrentAction = "Done"
		record.presentation.FinishedAt = manager.options.Clock()
		state.releaseReservations(record)
		manager.bump(record)
		return
	}
	record.presentation.Phase = fix.PhasePublishing
	record.presentation.Attention = fix.AttentionNone
	record.presentation.CurrentAction = "Finishing"
	if manager.bump(record) {
		manager.startCandidateCleanup(record, *record.candidate)
	}
}

func (manager *controller) startPublicationWorker(record *jobRecord, step publicationStep) {
	state := manager.state
	ctx, cancel := context.WithCancel(context.Background())
	record.cancel, record.publicationStep = cancel, step
	state.otherRunning++
	if !manager.bump(record) {
		state.otherRunning--
		record.cancel = nil
		cancel()
		return
	}
	input, identity := cloneFixInput(record.input), *record.candidate
	delivered, published := record.delivery, record.published
	go manager.publication.runPublicationStep(ctx, step, input, record.presentation.ID, record.attempt, identity, record.diffHash, sortedDiffPaths(record.diffPaths), delivered, published)
}
