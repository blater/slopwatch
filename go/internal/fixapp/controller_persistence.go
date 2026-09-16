package fixapp

import (
	"context"
	"encoding/json"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
	"github.com/blater/slopwatch/internal/publisher"
)

type persistenceOwner struct {
	store jobstore.Store
}

func (owner *persistenceOwner) saveRecord(ctx context.Context, record *jobRecord) error {
	state := storedJobStateFor(record)
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return owner.store.Save(ctx, jobstore.Record{JobID: record.presentation.ID, UpdatedAt: record.presentation.UpdatedAt, State: encoded})
}

func storedJobStateFor(record *jobRecord) storedJobState {
	diffPaths := sortedDiffPaths(record.diffPaths)
	return storedJobState{Presentation: clonePresentation(record.presentation), Input: storedJobInputFrom(record.input),
		Candidate: record.candidate, DiffHash: record.diffHash, DiffPaths: diffPaths, BaseScope: record.baseScope,
		Delivery: record.delivery, Published: record.published, Canceled: record.canceled,
		PublicationStep: record.publicationStep, Attempt: record.attempt, Commands: cloneReceipts(record.commands)}
}

type storedJobState struct {
	Presentation    fix.JobPresentation              `json:"presentation"`
	Input           storedJobInput                   `json:"input"`
	Candidate       *fix.CandidateIdentity           `json:"candidate,omitempty"`
	DiffHash        string                           `json:"diff_hash,omitempty"`
	DiffPaths       []fix.RepoPath                   `json:"diff_paths,omitempty"`
	BaseScope       fix.ScopeState                   `json:"base_scope,omitempty"`
	Delivery        delivery.Result                  `json:"delivery,omitempty"`
	Published       publisher.Result                 `json:"published,omitempty"`
	Canceled        bool                             `json:"canceled,omitempty"`
	PublicationStep publicationStep                  `json:"publication_step,omitempty"`
	Attempt         fix.AttemptID                    `json:"attempt,omitempty"`
	Commands        map[fix.CommandID]CommandReceipt `json:"commands,omitempty"`
}

type storedJobInput struct {
	Workspace           fix.WorkspaceIdentity    `json:"workspace"`
	Targets             []fix.RepoPath           `json:"targets"`
	TargetScore         float64                  `json:"target_score"`
	ChangeScope         string                   `json:"change_scope"`
	AllowedPaths        []fix.RepoPath           `json:"allowed_paths"`
	DeliveryPlan        fix.DeliveryPlan         `json:"delivery_plan"`
	DeliveryTarget      delivery.PreflightResult `json:"delivery_target"`
	BranchName          string                   `json:"branch_name"`
	DeliveryPreferences appconfig.Delivery       `json:"delivery_preferences"`
}

func storedJobInputFrom(input FixInput) storedJobInput {
	return storedJobInput{
		Workspace: input.Workspace, Targets: append([]fix.RepoPath(nil), input.Targets...), TargetScore: input.TargetScore,
		ChangeScope: input.ChangeScope, AllowedPaths: append([]fix.RepoPath(nil), input.AllowedPaths...), DeliveryPlan: input.DeliveryPlan,
		DeliveryTarget: input.DeliveryTarget, BranchName: input.BranchName, DeliveryPreferences: input.Preferences.Delivery,
	}
}

func (input storedJobInput) fixInput() FixInput {
	return FixInput{
		Workspace: input.Workspace, Targets: append([]fix.RepoPath(nil), input.Targets...), TargetScore: input.TargetScore,
		ChangeScope: input.ChangeScope, AllowedPaths: append([]fix.RepoPath(nil), input.AllowedPaths...), DeliveryPlan: input.DeliveryPlan,
		DeliveryTarget: input.DeliveryTarget, BranchName: input.BranchName, Preferences: appconfig.Resolved{Delivery: input.DeliveryPreferences},
	}
}

func cloneReceipts(source map[fix.CommandID]CommandReceipt) map[fix.CommandID]CommandReceipt {
	if len(source) == 0 {
		return nil
	}
	result := make(map[fix.CommandID]CommandReceipt, len(source))
	for id, receipt := range source {
		result[id] = receipt
	}
	return result
}

func jobRecordFromStored(stored jobstore.Record) (*jobRecord, bool) {
	var envelope storedJobState
	if json.Unmarshal(stored.State, &envelope) != nil || envelope.Presentation.ID == "" {
		return nil, false
	}
	record := &jobRecord{
		presentation: clonePresentation(envelope.Presentation), input: envelope.Input.fixInput(), attempt: envelope.Attempt,
		commands: cloneReceipts(envelope.Commands), diffHash: envelope.DiffHash, baseScope: envelope.BaseScope,
		delivery: envelope.Delivery, published: envelope.Published, canceled: envelope.Canceled,
		publicationStep: envelope.PublicationStep, storedAt: stored.UpdatedAt,
		resultLogged: isQuiescent(envelope.Presentation.Phase), candidate: cloneCandidate(envelope.Candidate),
		diffPaths: make(map[fix.RepoPath]bool, len(envelope.DiffPaths)),
	}
	if record.commands == nil {
		record.commands = map[fix.CommandID]CommandReceipt{}
	}
	if record.presentation.AttemptOrdinal <= 0 {
		record.presentation.AttemptOrdinal = 1
	}
	for _, path := range envelope.DiffPaths {
		record.diffPaths[path] = true
	}
	return record, true
}

func cloneCandidate(value *fix.CandidateIdentity) *fix.CandidateIdentity {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
