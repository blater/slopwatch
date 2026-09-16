package ghcli

import (
	"context"
	"errors"
	"strconv"

	"github.com/blater/slopwatch/internal/publisher"
)

func (service *Service) Create(ctx context.Context, request publisher.Request) (publisher.Result, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if request.HostRepository == "" || request.BaseBranch == "" || request.HeadBranch == "" || request.Commit == "" {
		return publisher.Result{}, errors.New("GitHub pull request request is incomplete")
	}
	for _, value := range []string{request.HostRepository, request.BaseBranch, request.HeadBranch, request.Title, request.Body} {
		if service.client.containsKnownSecret([]byte(value)) {
			return publisher.Result{}, errors.New("GitHub pull request request contains unsafe authentication material [REDACTED]")
		}
	}
	if existing, found, err := lookup(ctx, service.client, request, 0); err != nil {
		return publisher.Result{}, err
	} else if found {
		return existing, nil
	}
	arguments := []string{"pr", "create", "--repo", request.HostRepository, "--base", request.BaseBranch, "--head", request.HeadBranch,
		"--title", request.Title, "--body", request.Body}
	if request.Draft {
		arguments = append(arguments, "--draft")
	}
	run, err := service.client.run(ctx, arguments...)
	if err != nil {
		result := publisher.Result{Ambiguous: true, Diagnostic: "GitHub pull request creation may have taken effect"}
		if value := lastNonemptyLine(string(run.Stdout)); value != "" {
			if number, parseErr := pullRequestNumber(value, request.HostRepository); parseErr == nil {
				result.ProviderID, result.URL = strconv.Itoa(number), value
			}
		}
		return result, err
	}
	url := lastNonemptyLine(string(run.Stdout))
	if url == "" {
		return publisher.Result{Ambiguous: true, Diagnostic: "GitHub CLI returned no pull request URL"}, errors.New("GitHub pull request creation response was ambiguous")
	}
	number, parseErr := pullRequestNumber(url, request.HostRepository)
	if parseErr != nil {
		return publisher.Result{Ambiguous: true, Diagnostic: "GitHub CLI returned an invalid pull request URL"}, parseErr
	}
	result := publisher.Result{ProviderID: strconv.Itoa(number), URL: url, Draft: request.Draft}
	verified, found, reconcileErr := lookup(ctx, service.client, request, number)
	if reconcileErr != nil || !found {
		result.Ambiguous = true
		result.Diagnostic = "created pull request could not be reconciled"
		return result, errors.Join(errors.New(result.Diagnostic), reconcileErr)
	}
	return verified, nil
}
