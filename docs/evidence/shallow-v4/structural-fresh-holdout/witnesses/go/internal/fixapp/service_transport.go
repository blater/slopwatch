package fixapp

import (
	"bytes"
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
)

func (manager *Manager) Run(ctx context.Context, input FixInput) (fix.JobID, error) {
	response := make(chan runResponse, 1)
	if err := manager.send(ctx, runCall{ctx: ctx, input: cloneFixInput(input), response: response}); err != nil {
		return "", err
	}
	select {
	case result := <-response:
		return result.id, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-manager.controller.done:
		return "", ErrClosed
	}
}

func (manager *Manager) Execute(ctx context.Context, command fix.JobCommand) (CommandReceipt, error) {
	response := make(chan commandResponse, 1)
	if err := manager.send(ctx, commandCall{ctx: ctx, command: command, response: response}); err != nil {
		return CommandReceipt{}, err
	}
	select {
	case result := <-response:
		return result.receipt, result.err
	case <-ctx.Done():
		return CommandReceipt{}, ctx.Err()
	case <-manager.controller.done:
		return CommandReceipt{}, ErrClosed
	}
}

func (manager *Manager) send(ctx context.Context, request any) error {
	if manager.controller.closed.Load() {
		return ErrClosed
	}
	select {
	case manager.controller.requests <- request:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-manager.controller.done:
		return ErrClosed
	}
}

func (manager *Manager) Jobs(filter JobFilter) JobListSnapshot {
	response := make(chan JobListSnapshot, 1)
	if err := manager.send(context.Background(), jobsCall{filter: filter, response: response}); err != nil {
		return JobListSnapshot{}
	}
	select {
	case result := <-response:
		return result
	case <-manager.controller.done:
		return JobListSnapshot{}
	}
}

func (manager *Manager) Job(id fix.JobID) (fix.JobPresentation, bool) {
	for _, job := range manager.Jobs(JobFilter{IncludeFinished: true}).Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return fix.JobPresentation{}, false
}

func (manager *Manager) Subscribe() Subscription {
	manager.controller.notifyMu.Lock()
	notify := manager.controller.notify
	manager.controller.notifyMu.Unlock()
	return &subscription{manager: manager, notify: notify, closed: make(chan struct{})}
}

func (manager *Manager) Shutdown(ctx context.Context) error {
	if manager.controller.closed.Load() {
		return nil
	}
	response := make(chan error, 1)
	if err := manager.send(ctx, shutdownCall{response: response}); err != nil {
		if errors.Is(err, ErrClosed) {
			return nil
		}
		return err
	}
	select {
	case err := <-response:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (manager *Manager) CandidateFile(ctx context.Context, id fix.JobID, path fix.RepoPath) (candidate.File, error) {
	identity, previewBytes, previewLines, err := manager.candidateIdentity(ctx, id)
	if err != nil {
		return candidate.File{}, err
	}
	file, err := manager.controller.deps.Candidates.ReadFile(ctx, identity, path, previewBytes)
	if err != nil {
		return candidate.File{}, err
	}
	if previewLines <= 0 {
		return candidate.File{}, errors.New("candidate preview line limit is not configured")
	}
	lines := bytes.Split(file.Contents, []byte("\n"))
	if len(lines) > previewLines {
		file.Contents, file.Truncated = bytes.Join(lines[:previewLines], []byte("\n")), true
	}
	return file, nil
}

func (manager *Manager) Diff(ctx context.Context, id fix.JobID, request DiffRequest) (DiffPage, error) {
	identity, _, _, err := manager.candidateIdentity(ctx, id)
	if err != nil {
		return DiffPage{}, err
	}
	snapshot, err := manager.controller.deps.Candidates.Diff(ctx, identity)
	if err != nil {
		return DiffPage{}, err
	}
	offset := max(0, min(request.Offset, len(snapshot.Files)))
	limit := request.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	end := min(len(snapshot.Files), offset+limit)
	return DiffPage{Files: append([]candidate.DiffFile(nil), snapshot.Files[offset:end]...), Offset: offset,
		NextOffset: end, Complete: end == len(snapshot.Files), Fingerprint: snapshot.Fingerprint}, nil
}

func (manager *Manager) candidateIdentity(ctx context.Context, id fix.JobID) (fix.CandidateIdentity, int64, int, error) {
	response := make(chan candidateResponse, 1)
	if err := manager.send(ctx, candidateCall{id: id, response: response}); err != nil {
		return fix.CandidateIdentity{}, 0, 0, err
	}
	select {
	case result := <-response:
		if !result.ok {
			return fix.CandidateIdentity{}, 0, 0, ErrJobNotFound
		}
		return result.identity, result.previewBytes, result.previewLines, nil
	case <-ctx.Done():
		return fix.CandidateIdentity{}, 0, 0, ctx.Err()
	}
}

func (manager *Manager) Transcript(ctx context.Context, id fix.JobID, cursor LogCursor, limit int) (LogPage, error) {
	response := make(chan transcriptResponse, 1)
	if err := manager.send(ctx, transcriptCall{id: id, cursor: cursor, limit: limit, response: response}); err != nil {
		return LogPage{}, err
	}
	select {
	case result := <-response:
		return result.page, result.err
	case <-ctx.Done():
		return LogPage{}, ctx.Err()
	case <-manager.controller.done:
		return LogPage{}, ErrClosed
	}
}
