package fixapp

import (
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
)

func (manager *controller) handlePublicationResult(record *jobRecord, result workerResult) {
	record.cancel = nil
	record.publicationStep = ""
	record.delivery, record.published = result.delivery, result.published
	if result.deliveryTarget != (delivery.PreflightResult{}) {
		record.input.DeliveryTarget = result.deliveryTarget
	}
	setDeliveryState(record)
	if record.presentation.Phase == fix.PhaseCanceling {
		manager.handleCanceledPublication(record, result)
		return
	}
	if result.err != nil {
		manager.handlePublicationFailure(record, result)
		return
	}
	if manager.bump(record) {
		manager.startNextPublication(record)
	}
}

func setDeliveryState(record *jobRecord) {
	switch {
	case record.delivery.Ambiguous || record.published.Ambiguous:
		record.presentation.Delivery = fix.DeliveryAmbiguous
	case record.published.URL != "":
		record.presentation.Delivery = fix.DeliveryPullRequest
	case record.delivery.Pushed:
		record.presentation.Delivery = fix.DeliveryPushed
	case record.delivery.Commit != "":
		record.presentation.Delivery = fix.DeliveryCommitted
	}
}

func (manager *controller) handleCanceledPublication(record *jobRecord, result workerResult) {
	if result.delivery.Ambiguous || result.published.Ambiguous {
		record.presentation.Phase = fix.PhaseReconciling
		record.presentation.Attention = fix.AttentionNone
		record.presentation.CurrentAction = "Checking canceled delivery"
		record.presentation.Issue = &fix.JobIssue{Code: "publication_canceled", Summary: "Cancellation stopped publication after an external change may have occurred"}
		if manager.bump(record) {
			manager.startNextPublication(record)
		}
		return
	}
	if manager.bump(record) {
		manager.finishCancellation(record)
	}
}

func (manager *controller) handlePublicationFailure(record *jobRecord, result workerResult) {
	if result.delivery.Ambiguous || result.published.Ambiguous {
		record.presentation.Attention = fix.AttentionBlocking
		record.presentation.Issue = &fix.JobIssue{Code: "publication_ambiguous", Summary: "Publication state is ambiguous", Detail: result.err.Error()}
		record.presentation.Phase = fix.PhaseFailed
		record.presentation.CurrentAction = "Publication requires reconciliation"
		manager.bump(record)
		return
	}
	manager.failRecord(record, "publication_failed", result.err)
}
