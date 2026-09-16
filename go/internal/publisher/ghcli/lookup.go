package ghcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/blater/slopwatch/internal/publisher"
)

type prRecord struct {
	Number            int    `json:"number"`
	URL               string `json:"url"`
	Draft             bool   `json:"isDraft"`
	BaseRefName       string `json:"baseRefName"`
	HeadRefName       string `json:"headRefName"`
	HeadRefOID        string `json:"headRefOid"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	State             string `json:"state"`
}

func lookup(ctx context.Context, client ghClient, request publisher.Request, expectedNumber int) (publisher.Result, bool, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	records, err := listPullRequests(ctx, client, request)
	if err != nil {
		return publisher.Result{}, false, err
	}
	var matches []publisher.Result
	for _, record := range records {
		if expectedNumber > 0 && record.Number == expectedNumber &&
			!recordMatchesRequest(record, request) {
			return publisher.Result{Ambiguous: true, Diagnostic: "saved pull request no longer matches the delivered state"}, false,
				errors.New("saved GitHub pull request identity does not match the delivered commit and review state")
		}
		if recordMatchesRequest(record, request) &&
			(expectedNumber == 0 || record.Number == expectedNumber) {
			if record.Number <= 0 {
				return publisher.Result{}, false, errors.New("GitHub pull request lookup returned an invalid number")
			}
			if err := validatePullRequestURL(record.URL, request.HostRepository, record.Number); err != nil {
				return publisher.Result{}, false, err
			}
			matches = append(matches, publisher.Result{ProviderID: strconv.Itoa(record.Number), URL: record.URL, Draft: record.Draft})
		}
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	if len(matches) > 1 {
		return publisher.Result{Ambiguous: true, Diagnostic: "multiple pull requests match the delivered state"}, false,
			errors.New("GitHub pull request identity is not unique")
	}
	if len(records) > 0 {
		return publisher.Result{Ambiguous: true, Diagnostic: "branch already has a pull request for another commit"}, false,
			errors.New("GitHub branch pull request does not match the delivered commit")
	}
	return publisher.Result{}, false, nil
}

func recordMatchesRequest(record prRecord, request publisher.Request) bool {
	return record.HeadRefOID == string(request.Commit) && record.Draft == request.Draft &&
		record.BaseRefName == request.BaseBranch && record.HeadRefName == request.HeadBranch &&
		!record.IsCrossRepository && strings.EqualFold(record.State, "OPEN")
}

func listPullRequests(ctx context.Context, client ghClient, request publisher.Request) ([]prRecord, error) {
	limit := 100
	for {
		run, err := client.run(ctx, "pr", "list", "--repo", request.HostRepository, "--state", "all", "--head", request.HeadBranch,
			"--json", "number,url,isDraft,baseRefName,headRefName,headRefOid,isCrossRepository,state", "--limit", strconv.Itoa(limit))
		if err != nil {
			return nil, err
		}
		var records []prRecord
		if err := json.Unmarshal(run.Stdout, &records); err != nil {
			return nil, fmt.Errorf("decode GitHub pull request lookup: %w", err)
		}
		if len(records) < limit {
			return records, nil
		}
		if limit > int(^uint(0)>>1)/2 {
			return nil, errors.New("GitHub pull request result window exceeds platform addressability")
		}
		limit *= 2
	}
}

func previousPullRequestNumber(previous publisher.Result, hostRepository string) (int, error) {
	var expected int
	if previous.ProviderID != "" {
		number, err := strconv.Atoi(previous.ProviderID)
		if err != nil || number <= 0 {
			return 0, errors.New("saved GitHub pull request number is invalid")
		}
		expected = number
	}
	if previous.URL != "" {
		number, err := pullRequestNumber(previous.URL, hostRepository)
		if err != nil || expected > 0 && number != expected {
			return 0, errors.New("saved GitHub pull request URL and number disagree")
		}
		expected = number
	}
	return expected, nil
}
