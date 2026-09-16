package delivery

import (
	"context"
	"errors"
	"strings"
)

func (executor gitExecutor) ref(ctx context.Context, root, ref string) (bool, string, error) {
	data, err := executor.bytes(ctx, root, true, "rev-parse", "--verify", "--quiet", ref)
	if errors.Is(err, errGitExitOne) {
		return false, "", nil
	}
	return err == nil, strings.TrimSpace(string(data)), err
}

func (executor gitExecutor) remoteRef(ctx context.Context, root, remote, ref string) (bool, string, error) {
	remoteURL, err := executor.resolveRemoteURL(ctx, root, remote)
	if err != nil {
		return false, "", err
	}
	return executor.remoteRefURL(ctx, root, remoteURL, ref)
}

func (executor gitExecutor) remoteRefURL(ctx context.Context, root, remoteURL, ref string) (bool, string, error) {
	data, err := executor.bytes(ctx, root, true, "ls-remote", "--exit-code", "--refs", remoteURL, ref)
	if errors.Is(err, errGitExitOne) || (err != nil && strings.Contains(err.Error(), "status 2")) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return false, "", nil
	}
	if len(fields) != 2 || fields[1] != ref {
		return false, "", errors.New("malformed remote ref response")
	}
	return true, fields[0], nil
}
