package analysiscache

import (
	"context"
	"github.com/blater/slopwatch/internal/sourcefs"
)

type snapshotFileSystemKey struct{}

// WithSnapshotFileSystem instruments materialization separately from planning.
func WithSnapshotFileSystem(ctx context.Context, fs sourcefs.FileSystem) context.Context {
	return context.WithValue(ctx, snapshotFileSystemKey{}, fs)
}
func snapshotFileSystem(ctx context.Context) sourcefs.FileSystem {
	fs, _ := ctx.Value(snapshotFileSystemKey{}).(sourcefs.FileSystem)
	return sourcefs.Default(fs)
}
