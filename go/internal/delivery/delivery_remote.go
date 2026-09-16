package delivery

import (
	"context"
	"errors"
)

func (executor gitExecutor) resolveRemoteURL(ctx context.Context, root, remote string) (string, error) {
	if !validRemoteAlias(remote) {
		return "", errors.New("delivery remote must be a safe configured remote alias")
	}
	value, err := executor.text(ctx, root, "remote", "get-url", "--push", remote)
	if err != nil {
		return "", err
	}
	if err := validateRemoteURL(value); err != nil {
		return "", err
	}
	return canonicalLocalRemote(value)
}
