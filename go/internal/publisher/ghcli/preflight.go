package ghcli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blater/slopwatch/internal/publisher"
)

func (service *Service) Preflight(ctx context.Context, request publisher.PreflightRequest) (publisher.Readiness, error) {
	ctx = withCommandOutput(ctx, request.CommandOutputBytes)
	if request.Provider != ProviderID {
		return publisher.Readiness{}, fmt.Errorf("GitHub publisher does not support provider %q", request.Provider)
	}
	if !strings.EqualFold(request.RemoteHost, "github.com") || request.HostRepository == "" {
		return publisher.Readiness{}, errors.New("GitHub publisher requires a verified github.com owner/repository remote")
	}
	if service.client.containsKnownSecret([]byte(request.HostRepository)) {
		return publisher.Readiness{}, errors.New("GitHub publisher request contains unsafe authentication material [REDACTED]")
	}
	if _, err := service.client.run(ctx, "auth", "status", "--hostname", "github.com"); err != nil {
		return publisher.Readiness{}, fmt.Errorf("GitHub publisher authentication is not ready: %w", err)
	}
	result, err := service.client.run(ctx, "repo", "view", request.HostRepository, "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err != nil {
		return publisher.Readiness{}, fmt.Errorf("GitHub repository is not ready for publication: %w", err)
	}
	if strings.TrimSpace(string(result.Stdout)) != request.HostRepository {
		return publisher.Readiness{}, errors.New("GitHub publisher resolved a different repository")
	}
	return publisher.Readiness{Provider: ProviderID, HostRepository: request.HostRepository}, nil
}
