// Package ghcli adapts GitHub CLI pull-request creation behind the publisher port.
package ghcli

type Service struct {
	client ghClient
}
