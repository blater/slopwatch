package ghcli

import (
	"context"

	"github.com/blater/slopwatch/internal/isolation"
)

type commandOutputContextKey struct{}

func withCommandOutput(ctx context.Context, maximum int64) context.Context {
	return context.WithValue(ctx, commandOutputContextKey{}, maximum)
}

const ProviderID = "github-cli"

type Config struct {
	Executable       string
	WorkingDirectory string
}

func New(config Config, runner isolation.Executor) (*Service, error) {
	client, err := newGHClient(config, runner)
	if err != nil {
		return nil, err
	}
	return &Service{client: client}, nil
}
