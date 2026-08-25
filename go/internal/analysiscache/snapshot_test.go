package analysiscache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMaterializeSnapshotIsReadOnlyAndUsesVerifiedBlobs(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	goSource := []byte("package pkg\n")
	config := []byte("module example\n")
	goDigest, err := store.PutSource(goSource)
	if err != nil {
		t.Fatal(err)
	}
	configDigest, err := store.PutSource(config)
	if err != nil {
		t.Fatal(err)
	}
	root, cleanup, err := store.MaterializeSnapshot(context.Background(), []SnapshotFile{
		{Path: "go.mod", Digest: configDigest},
		{Path: "pkg/source.go", Digest: goDigest},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanup() })
	assertSnapshotContents(t, root, map[string][]byte{"go.mod": config, "pkg/source.go": goSource})
	assertSnapshotReadOnly(t, root)
	assertSnapshotCleanup(t, root, cleanup)
}

func assertSnapshotContents(t *testing.T, root string, expected map[string][]byte) {
	t.Helper()
	for path, want := range expected {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		got, readErr := os.ReadFile(fullPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != string(want) {
			t.Fatalf("snapshot %s = %q, want %q", path, got, want)
		}
		info, statErr := os.Stat(fullPath)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Fatalf("snapshot file %s is writable: %o", path, info.Mode().Perm())
		}
	}
}

func assertSnapshotReadOnly(t *testing.T, root string) {
	t.Helper()
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if rootInfo.Mode().Perm()&0o222 != 0 {
		t.Fatalf("snapshot root is writable: %o", rootInfo.Mode().Perm())
	}
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(filepath.Join(root, "pkg", "source.go"), []byte("mutated"), 0o600); err == nil {
			t.Fatal("read-only snapshot file accepted a mutation")
		}
	}
}

func assertSnapshotCleanup(t *testing.T, root string, cleanup func() error) {
	t.Helper()
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot still exists after cleanup: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("idempotent cleanup failed: %v", err)
	}
}

func TestMaterializeSnapshotRejectsEscapingAndInvalidPaths(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	digest, err := store.PutSource([]byte("safe"))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"", ".", "..", "../escape", "a/../../escape", filepath.Join(string(filepath.Separator), "absolute")}
	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			if root, cleanup, materializeErr := store.MaterializeSnapshot(context.Background(), []SnapshotFile{{Path: path, Digest: digest}}); materializeErr == nil {
				if cleanup != nil {
					_ = cleanup()
				}
				t.Fatalf("unsafe path %q materialized at %q", path, root)
			}
		})
	}
	if _, _, err := store.MaterializeSnapshot(context.Background(), []SnapshotFile{
		{Path: "same", Digest: digest}, {Path: "./same", Digest: digest},
	}); err == nil {
		t.Fatal("duplicate canonical snapshot path was accepted")
	}
}

func TestMaterializeSnapshotHonorsCancellationAndMissingBlobs(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.MaterializeSnapshot(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled materialization returned %v", err)
	}
	missing := DigestBytes([]byte("not stored"))
	if _, _, err := store.MaterializeSnapshot(context.Background(), []SnapshotFile{{Path: "a.go", Digest: missing}}); err == nil {
		t.Fatal("missing source blob was materialized")
	}
}

func TestMaterializeWorkspaceSnapshotVerifiesLiveContents(t *testing.T) {
	workspace := t.TempDir()
	contents := []byte("package sample\n")
	if err := os.WriteFile(filepath.Join(workspace, "sample.go"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	file := SnapshotFile{Path: "sample.go", Digest: DigestBytes(contents)}
	root, cleanup, err := store.MaterializeWorkspaceSnapshot(context.Background(), workspace, []SnapshotFile{file})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	got, err := os.ReadFile(filepath.Join(root, "sample.go"))
	if err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("materialized source = %q, %v", got, err)
	}
	file.Digest = DigestBytes([]byte("different"))
	if _, _, err := store.MaterializeWorkspaceSnapshot(context.Background(), workspace, []SnapshotFile{file}); !errors.Is(err, ErrWorkspaceSnapshotChanged) {
		t.Fatalf("changed workspace error = %v, want ErrWorkspaceSnapshotChanged", err)
	}
}

func BenchmarkMaterializeWorkspaceSnapshotTwentyFiveThousandFiles(b *testing.B) {
	workspace := b.TempDir()
	files := make([]SnapshotFile, 25_000)
	for index := range files {
		relative := fmt.Sprintf("pkg/%05d/source.go", index/5)
		if index%5 != 0 {
			relative = fmt.Sprintf("pkg/%05d/source_%d.go", index/5, index%5)
		}
		contents := []byte(fmt.Sprintf("package pkg%05d\n", index/5))
		path := filepath.Join(workspace, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			b.Fatal(err)
		}
		files[index] = SnapshotFile{Path: relative, Digest: DigestBytes(contents)}
	}
	store, err := NewStore(filepath.Join(b.TempDir(), "cache"))
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		_, cleanup, materializeErr := store.MaterializeWorkspaceSnapshot(context.Background(), workspace, files)
		if materializeErr != nil {
			b.Fatal(materializeErr)
		}
		if cleanupErr := cleanup(); cleanupErr != nil {
			b.Fatal(cleanupErr)
		}
	}
}
