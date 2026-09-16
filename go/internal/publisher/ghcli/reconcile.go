package ghcli

import (
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/publisher"
)

func (service *Service) Reconcile(ctx context.Context, request publisher.Request, previous publisher.Result) (publisher.Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	expectedNumber, identityErr := previousPullRequestNumber(previous, request.HostRepository)
	if identityErr != nil {
		previous.Ambiguous = true
		previous.Diagnostic = "saved pull request identity is invalid"
		return previous, identityErr
	}
	result, found, err := lookup(ctx, service.client, request, expectedNumber)
	if err != nil {
		previous.Ambiguous = true
		previous.Diagnostic = err.Error()
		return previous, err
	}
	if !found {
		if expectedNumber > 0 {
			previous.Ambiguous = true
			previous.Diagnostic = "saved pull request is not visible for exact reconciliation"
			return previous, errors.New(previous.Diagnostic)
		}
		return publisher.Result{Diagnostic: "pull request is absent"}, nil
	}
	return result, nil
}
