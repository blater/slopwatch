package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"github.com/blater/slopwatch/internal/analysiscache"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCacheRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"sources", "units", "workspaces", "artifacts"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRun(t *testing.T) {
	for _, args := range [][]string{nil, {"other"}, {"compact"}, {"compact", "--root", "/does/not/exist"}, {"compact", "--unknown"}} {
		var out bytes.Buffer
		if run(context.Background(), args, &out, &out) == 0 {
			t.Fatalf("accepted %v", args)
		}
	}
	var out bytes.Buffer
	if code := run(context.Background(), []string{"compact", "--root", testCacheRoot(t)}, &out, &out); code != 0 || !strings.Contains(out.String(), "compacted=0") {
		t.Fatalf("%d %s", code, &out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := run(ctx, []string{"compact", "--root", testCacheRoot(t)}, &out, &out); code != 1 {
		t.Fatalf("cancellation code %d", code)
	}
}

func TestRejectUnrelatedRoot(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	if run(context.Background(), []string{"compact", "--root", root}, &out, &out) != 2 {
		t.Fatal("accepted unrelated root")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("modified unrelated root")
	}
}

func TestCompactPopulatedLegacyCache(t *testing.T) {
	root := testCacheRoot(t)
	store, err := analysiscache.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	key := analysiscache.Key(analysiscache.DigestBytes([]byte("cli-unit")))
	unit := analysiscache.UnitArtifact{UnitKey: key, Language: strings.Repeat("go", 1000)}
	ref, err := store.PutUnit(key, unit)
	if err != nil {
		t.Fatal(err)
	}
	digest := string(ref.Digest)
	path := filepath.Join(root, "artifacts", digest[:2], digest[2:])
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, plain, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := run(context.Background(), []string{"compact", "--root", root}, &out, &out); code != 0 {
		t.Fatalf("%d %s", code, &out)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= int64(len(plain)) {
		t.Fatal("legacy cache did not shrink")
	}
	want := fmt.Sprintf("artifacts=1 compacted=1 skipped=0 errors=0 bytes_before=%d bytes_after=%d", len(plain), info.Size())
	if !strings.Contains(out.String(), want) {
		t.Fatalf("stats: %s; want %s", &out, want)
	}
	got, gotRef, ok := store.LoadUnitByKey(key)
	if !ok || gotRef != ref || got.Language != unit.Language {
		t.Fatal("CLI changed cached unit")
	}
}
