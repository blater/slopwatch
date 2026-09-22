//go:build darwin

package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestFSEventsStartsAboveDescriptorLimit(t *testing.T) {
	if os.Getenv("SLOPWATCH_FSEVENTS_FD_TEST") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestFSEventsStartsAboveDescriptorLimit$", "-test.v")
		cmd.Env = append(os.Environ(), "SLOPWATCH_FSEVENTS_FD_TEST=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("descriptor regression: %v\n%s", err, output)
		} else {
			t.Logf("%s", output)
		}
		return
	}
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	limit.Cur = min(limit.Max, 256)
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for dir := 0; dir < 16; dir++ {
		path := filepath.Join(root, fmt.Sprintf("p%d", dir))
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		for file := 0; file < 64; file++ {
			if err := os.WriteFile(filepath.Join(path, fmt.Sprintf("f%d.go", file)), []byte("package p\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	countFDs := func() int {
		files, err := os.ReadDir("/dev/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(files)
	}
	// FSEvents/libdispatch initializes process-wide handles on first use and
	// retains them. Measure subsequent subscriptions against that baseline.
	warm, err := newPlatformBackend()
	if err != nil {
		t.Fatal(err)
	}
	if err := warm.Add(root); err != nil {
		t.Fatal(err)
	}
	if err := warm.Close(); err != nil {
		t.Fatal(err)
	}
	before := countFDs()
	started := time.Now()
	m, err := New(Config{Root: root, Classifier: ClassifierFunc(sourceClassifier), Debounce: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	backend := m.engine.watch.backend.(*bufferedBackend).Backend.(*fseventsBackend)
	if len(backend.subscriptions) != 1 {
		t.Fatalf("native subscriptions=%d, want 1", len(backend.subscriptions))
	}
	after := countFDs()
	if after-before > 16 {
		t.Fatalf("1024 files added %d descriptors", after-before)
	}
	t.Logf("1024 files, limit=%d, startup=%s, descriptors=%d -> %d, subscriptions=1", limit.Cur, time.Since(started), before, after)
	path := filepath.Join(root, "p0", "f0.go")
	if err := os.WriteFile(path, []byte("package changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		batch, err := m.WaitAndDrain(ctx)
		if err != nil {
			t.Fatalf("real edit not delivered: %v", err)
		}
		found := false
		for _, entry := range batch.Entries {
			if entry.Path == "p0/f0.go" {
				found = true
			}
		}
		if found {
			break
		}
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if released := countFDs(); released > before+4 {
		t.Fatalf("descriptors not released: before=%d after close=%d", before, released)
	}
}
