package candidate

import (
	"context"
	"os"
	"path/filepath"

	"github.com/blater/slopwatch/internal/fix"
)

func (service *DirectService) Discard(_ context.Context, identity fix.CandidateIdentity) error {
	if _, err := loadDirectRecord(service.stateRoot, identity); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(service.stateRoot, string(identity.Job)))
}

func (service *DirectService) Release(ctx context.Context, identity fix.CandidateIdentity) error {
	return service.Discard(ctx, identity)
}

func (service *DirectService) Close() error { return nil }
