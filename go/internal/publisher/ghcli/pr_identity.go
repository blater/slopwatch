package ghcli

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

func validatePullRequestURL(value, hostRepository string, expectedNumber int) error {
	number, err := pullRequestNumber(value, hostRepository)
	if err != nil || expectedNumber > 0 && number != expectedNumber {
		return errors.New("GitHub CLI returned an invalid pull request URL")
	}
	return nil
}

func pullRequestNumber(value, hostRepository string) (int, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.Port() != "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(value, "\x00\r\n\t ") {
		return 0, errors.New("GitHub CLI returned an invalid pull request URL")
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] != "pull" || strings.Contains(parts[0]+parts[1], "%") {
		return 0, errors.New("GitHub CLI returned an invalid pull request URL")
	}
	number, err := strconv.Atoi(parts[3])
	requested := strings.Split(hostRepository, "/")
	if err != nil || number <= 0 || len(requested) != 2 || !strings.EqualFold(parts[0], requested[0]) ||
		!strings.EqualFold(parts[1], requested[1]) {
		return 0, errors.New("GitHub CLI returned an invalid pull request URL")
	}
	return number, nil
}
