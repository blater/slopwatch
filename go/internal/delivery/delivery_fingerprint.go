package delivery

import (
	"context"

	"github.com/blater/slopwatch/internal/gitmanifest"
)

func (service *GitService) diffFingerprint(ctx context.Context, root string) (string, error) {
	data, err := service.executor.bytes(ctx, root, false, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	manifest, err := gitmanifest.Build(root, data)
	if err != nil {
		return "", err
	}
	return manifest.Fingerprint, nil
}
