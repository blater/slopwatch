package workspace

import (
	"encoding/json"
	"fmt"
	"github.com/blater/slopwatch/internal/sourcefs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type countedInventoryFS struct {
	sourcefs.OS
	reads map[string]int
}

func (fs *countedInventoryFS) ReadDir(path string) ([]os.DirEntry, error) {
	fs.reads[path]++
	return fs.OS.ReadDir(path)
}

func TestNamedEventsDoNotRediscoverKnownDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fs := &countedInventoryFS{reads: map[string]int{}}
	monitor := newTestMonitor(t, root, newFakeBackend(), Config{FileSystem: fs})
	clear(fs.reads)
	monitor.handle(Event{Name: "a.go", Op: OpWrite})
	monitor.handle(Event{Name: root, Op: OpCreate, IsDir: true})
	if len(fs.reads) != 0 {
		t.Fatalf("known paths enumerated: %v", fs.reads)
	}
	dir := filepath.Join(root, "new")
	if err := os.MkdirAll(filepath.Join(dir, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child", "b.go"), []byte("package b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	monitor.handle(Event{Name: dir, Op: OpCreate, IsDir: true})
	monitor.handle(Event{Name: filepath.Join(dir, "child"), Op: OpCreate, IsDir: true})
	monitor.handle(Event{Name: dir, Op: OpCreate, IsDir: true})
	if len(fs.reads) != 2 || fs.reads[dir] != 1 || fs.reads[filepath.Join(dir, "child")] != 1 {
		t.Fatalf("subtree enumerations: %v", fs.reads)
	}
	clear(fs.reads)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	monitor.handle(Event{Name: dir, Op: OpRemove, IsDir: true})
	if len(fs.reads) != 0 {
		t.Fatalf("deletion enumerated: %v", fs.reads)
	}
	if _, ok := monitor.engine.watch.known[filepath.Join(dir, "child", "b.go")]; ok {
		t.Fatal("deleted source retained")
	}
}

func TestOverlappingScopesUpgradeNonrecursiveInventory(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(child, "a.go")
	if err := os.WriteFile(source, []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	monitor := newTestMonitor(t, root, newFakeBackend(), Config{Scopes: []Scope{{Path: root, Directory: true}, {Path: root, Directory: true, Recursive: true}}})
	if !monitor.isWatched(child) {
		t.Fatal("recursive scope did not register child")
	}
	if _, ok := monitor.engine.watch.known[source]; !ok {
		t.Fatal("recursive scope missed source")
	}
}

func TestDirectoryReplacementUsesFinalKindAndRetainedChildren(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "branch.go")
	if err := os.MkdirAll(filepath.Join(path, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(path, "child", "old.go")
	if err := os.WriteFile(old, []byte("package old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fs := &countedInventoryFS{reads: map[string]int{}}
	monitor := newTestMonitor(t, root, newFakeBackend(), Config{FileSystem: fs})
	clear(fs.reads)
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package replacement\n"), 0600); err != nil {
		t.Fatal(err)
	}
	monitor.handle(Event{Name: path, Op: OpRemove | OpCreate, IsDir: true})
	if len(fs.reads) != 0 {
		t.Fatal("directory-to-file replacement enumerated")
	}
	if _, ok := monitor.engine.watch.known[old]; ok {
		t.Fatal("old descendant survived directory-to-file replacement")
	}
	if _, ok := monitor.engine.watch.known[path]; !ok {
		t.Fatal("replacement source missing")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(path, "new.go")
	if err := os.WriteFile(next, []byte("package next\n"), 0600); err != nil {
		t.Fatal(err)
	}
	monitor.handle(Event{Name: path, Op: OpRemove | OpCreate})
	if _, ok := monitor.engine.watch.known[path]; ok {
		t.Fatal("old file survived file-to-directory replacement")
	}
	if _, ok := monitor.engine.watch.known[next]; !ok {
		t.Fatal("new descendant missing")
	}
	if fs.reads[path] != 1 {
		t.Fatalf("replacement subtree reads=%v", fs.reads)
	}
	// Same-kind replacement evidence must invalidate completion, too.
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(path, "final.go")
	if err := os.WriteFile(final, []byte("package final\n"), 0600); err != nil {
		t.Fatal(err)
	}
	monitor.handle(Event{Name: path, Op: OpRemove | OpCreate, IsDir: true})
	if _, ok := monitor.engine.watch.known[next]; ok {
		t.Fatal("old member survived directory replacement")
	}
	if _, ok := monitor.engine.watch.known[final]; !ok {
		t.Fatal("final directory member missing")
	}
}

func TestDirectoryWorkDoesNotGrowWithUnrelatedInventory(t *testing.T) {
	for _, size := range []int{8, 30000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			root := t.TempDir()
			for i := 0; i < size; i++ {
				path := filepath.Join(root, fmt.Sprintf("p%05d", i))
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "a.go"), []byte("package a\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			fs := &countedInventoryFS{reads: map[string]int{}}
			monitor := newTestMonitor(t, root, newFakeBackend(), Config{FileSystem: fs})
			clear(fs.reads)
			subtree := filepath.Join(root, "new")
			if err := os.MkdirAll(filepath.Join(subtree, "child"), 0700); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(subtree, "child", "b.go")
			if err := os.WriteFile(source, []byte("package b\n"), 0600); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			monitor.handle(Event{Name: subtree, Op: OpCreate, IsDir: true})
			created := time.Since(started)
			if len(fs.reads) != 2 || fs.reads[subtree] != 1 || fs.reads[filepath.Dir(source)] != 1 {
				t.Fatalf("new subtree reads=%v", fs.reads)
			}
			clear(fs.reads)
			if err := os.RemoveAll(subtree); err != nil {
				t.Fatal(err)
			}
			started = time.Now()
			monitor.handle(Event{Name: subtree, Op: OpRemove, IsDir: true})
			removed := time.Since(started)
			if len(fs.reads) != 0 {
				t.Fatalf("deleted subtree enumerated=%v", fs.reads)
			}
			if len(monitor.engine.watch.known) != size {
				t.Fatalf("unrelated inventory changed: %d", len(monitor.engine.watch.known))
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(cwd, "..", "..", "..", "build", fmt.Sprintf("incremental-watch-%d.json", size))
			if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
				t.Fatal(err)
			}
			data, _ := json.MarshalIndent(map[string]any{"startup_sources": size, "subtree_directories": 2, "subtree_sources": 1, "create_directory_reads": 2, "delete_directory_reads": 0, "create_ns": created.Nanoseconds(), "delete_ns": removed.Nanoseconds()}, "", "  ")
			if err := os.WriteFile(output, data, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCoalescedAtomicSaveRetainsFinalSource(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("package before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fs := &countedInventoryFS{reads: map[string]int{}}
	monitor := newTestMonitor(t, root, newFakeBackend(), Config{FileSystem: fs})
	clear(fs.reads)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	monitor.handle(Event{Name: path, Op: OpRemove | OpCreate | OpWrite})
	if _, ok := monitor.engine.watch.known[path]; !ok {
		t.Fatal("final recreated source was forgotten")
	}
	batch := monitor.Drain()
	if len(batch.Entries) != 1 || batch.Entries[0].Reasons&(ReasonRemove|ReasonCreate|ReasonWrite) != (ReasonRemove|ReasonCreate|ReasonWrite) {
		t.Fatalf("coalesced replacement evidence lost: %+v", batch)
	}
	if len(fs.reads) != 0 {
		t.Fatal("atomic source save enumerated directory")
	}
}

func TestDirectorySymlinkEventsRespectFollowPolicy(t *testing.T) {
	for _, follow := range []bool{false, true} {
		for _, replacement := range []bool{false, true} {
			t.Run(fmt.Sprintf("follow=%t/replacement=%t", follow, replacement), func(t *testing.T) {
				root, outside := t.TempDir(), t.TempDir()
				writeDirectoryFile(t, outside, "new.go")
				if replacement {
					writeDirectoryFile(t, root, "link/old.go")
				}
				fs := &countedInventoryFS{reads: map[string]int{}}
				m := newTestMonitor(t, root, newFakeBackend(), Config{FollowSymlinks: follow, FileSystem: fs})
				clear(fs.reads)
				link := filepath.Join(root, "link")
				op := OpCreate
				if replacement {
					if err := os.RemoveAll(link); err != nil {
						t.Fatal(err)
					}
					op |= OpRemove
				}
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}
				m.handle(Event{Name: link, Op: op})
				batch := m.Drain()
				want := []string{}
				if follow {
					want = append(want, "link/new.go")
				}
				if replacement {
					want = append(want, "link/old.go")
				}
				assertDirtyPaths(t, batch, want...)
				if m.isWatched(link) != follow {
					t.Fatalf("watched=%t follow=%t", m.isWatched(link), follow)
				}
				if !follow && len(fs.reads) != 0 {
					t.Fatalf("follow-disabled enumerated: %v", fs.reads)
				}
			})
		}
	}
}
func TestRootLossRequiresRestartWithoutRediscovery(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			root := t.TempDir()
			writeDirectoryFile(t, root, "branch/a.go")
			fs := &countedInventoryFS{reads: map[string]int{}}
			m := newTestMonitor(t, root, newFakeBackend(), Config{FileSystem: fs})
			clear(fs.reads)
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			if replacement {
				writeDirectoryFile(t, root, "new/b.go")
			}
			m.handle(Event{Name: root, Op: OpRemove | OpCreate})
			batch := m.Drain()
			if !batch.Closed || batch.Err == nil || len(batch.Entries) != 1 || batch.Entries[0].Path != "branch/a.go" || len(fs.reads) != 0 {
				t.Fatalf("root loss silently lost monitoring or rediscovered: %+v reads=%v", batch, fs.reads)
			}
		})
	}
}
