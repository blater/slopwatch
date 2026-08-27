package analysiscache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopmochi/internal/report"
)

type generationExpectation struct {
	key Key
	ref ArtifactRef
}

func TestStoreRoundTripAndPrivatePermissions(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	assertPrivateStoreDirectories(t, store)
	assertSourceRoundTrip(t, store)
	assertUnitRoundTrip(t, store)
}

func assertPrivateStoreDirectories(t *testing.T, store *Store) {
	t.Helper()
	info, err := os.Stat(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("root permissions = %o, want 700", got)
	}
	for _, directory := range []string{"sources", "artifacts", "units", "workspaces"} {
		info, statErr := os.Stat(filepath.Join(store.Root(), directory))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s permissions = %o, want 700", directory, got)
		}
	}
}

func assertSourceRoundTrip(t *testing.T, store *Store) {
	t.Helper()
	source := []byte("package cache\n")
	digest, err := store.PutSource(source)
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := store.LoadSource(digest)
	if !ok || !reflect.DeepEqual(loaded, source) {
		t.Fatalf("LoadSource() = %q, %v", loaded, ok)
	}
	secondDigest, err := store.PutSource(source)
	if err != nil || secondDigest != digest {
		t.Fatalf("content-addressed source identity = %s, %v; want %s", secondDigest, err, digest)
	}
}

func assertUnitRoundTrip(t *testing.T, store *Store) {
	t.Helper()
	unitKey := keyFor([]byte("unit"))
	artifact := sampleUnit(unitKey)
	ref, err := store.PutUnit(unitKey, artifact)
	if err != nil {
		t.Fatal(err)
	}
	secondRef, err := store.PutUnit(unitKey, artifact)
	if err != nil || secondRef != ref {
		t.Fatalf("content-addressed artifact identity = %#v, %v; want %#v", secondRef, err, ref)
	}
	got, ok := store.LoadUnit(ref, unitKey)
	if !ok || !reflect.DeepEqual(got, artifact) {
		t.Fatalf("LoadUnit() mismatch:\n got %#v\nwant %#v", got, artifact)
	}
	indexed, indexedRef, ok := store.LoadUnitByKey(unitKey)
	if !ok || indexedRef != ref || !reflect.DeepEqual(indexed, artifact) {
		t.Fatalf("LoadUnitByKey() = %#v, %#v, %v", indexed, indexedRef, ok)
	}
}

func TestCorruptUnitIndexIsCacheMiss(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"truncated", func(t *testing.T, path string) {
			if err := os.Truncate(path, 5); err != nil {
				t.Fatal(err)
			}
		}},
		{"schema mismatch", func(t *testing.T, path string) {
			mutateEnvelopeSchema(t, path)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStore(t)
			key := keyFor([]byte("indexed-unit"))
			if _, err := store.PutUnit(key, sampleUnit(key)); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, store.unitIndexPath(key))
			if _, _, ok := store.LoadUnitByKey(key); ok {
				t.Fatal("corrupt unit index was accepted")
			}
		})
	}
}

func mutateEnvelopeSchema(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored envelope
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	stored.Schema++
	data, err = json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentUnitIndexReadersObserveWholeArtifacts(t *testing.T) {
	store := newTestStore(t)
	key := keyFor([]byte("indexed-unit"))
	artifacts := []UnitArtifact{sampleUnit(key), sampleUnit(key)}
	artifacts[0].Language = "go"
	artifacts[1].Language = "rust"
	if _, err := store.PutUnit(key, artifacts[0]); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	errors := make(chan error, 16)
	var readers sync.WaitGroup
	for index := 0; index < 8; index++ {
		readers.Add(1)
		go readUnitIndexUntilStopped(store, key, stop, errors, &readers)
	}
	var writers sync.WaitGroup
	for writer := 0; writer < 4; writer++ {
		writer := writer
		writers.Add(1)
		go writeUnitIndex(store, key, artifacts, writer, errors, &writers)
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

func readUnitIndexUntilStopped(store *Store, key Key, stop <-chan struct{}, errors chan<- error, done *sync.WaitGroup) {
	defer done.Done()
	for {
		select {
		case <-stop:
			return
		default:
		}
		artifact, _, ok := store.LoadUnitByKey(key)
		if !ok || (artifact.Language != "go" && artifact.Language != "rust") {
			errors <- fmt.Errorf("observed torn unit index: %#v, %v", artifact, ok)
			return
		}
	}
}

func writeUnitIndex(store *Store, key Key, artifacts []UnitArtifact, writer int, errors chan<- error, done *sync.WaitGroup) {
	defer done.Done()
	for index := 0; index < 10; index++ {
		if _, err := store.PutUnit(key, artifacts[(writer+index)%len(artifacts)]); err != nil {
			errors <- err
			return
		}
	}
}

func TestProjectionPreservesDisplayDataAndOmitsEvidence(t *testing.T) {
	t.Parallel()
	workspace := keyFor([]byte("workspace"))
	unit := sampleUnit(keyFor([]byte("unit")))
	projection := ProjectionFromReport(workspace, unit.Report, FreshnessProvisional)
	if len(projection.Files) != 1 || projection.Files[0].Freshness != FreshnessProvisional {
		t.Fatalf("unexpected projection: %#v", projection)
	}
	files := projection.ReportFiles()
	component := files[0].Components["complexity"]
	if len(component.Evidence) != 0 || len(component.Waivers) != 0 {
		t.Fatal("compact projection retained heavyweight detail")
	}
	wantComponent := unit.Report.Files[0].Components["complexity"]
	if component.Contribution != wantComponent.Contribution ||
		!reflect.DeepEqual(component.Subjects, wantComponent.Subjects) ||
		files[0].Score != unit.Report.Files[0].Score {
		t.Fatal("compact projection lost display aggregate data")
	}
}

func TestCorruptArtifactsAreCacheMisses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(t *testing.T, data []byte) []byte
	}{
		{"truncated", func(_ *testing.T, data []byte) []byte { return data[:len(data)/2] }},
		{"checksum", func(t *testing.T, data []byte) []byte {
			var stored envelope
			if err := json.Unmarshal(data, &stored); err != nil {
				t.Fatal(err)
			}
			stored.Checksum = DigestBytes([]byte("wrong checksum"))
			result, err := json.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			return result
		}},
		{"schema mismatch", func(t *testing.T, data []byte) []byte {
			var stored envelope
			if err := json.Unmarshal(data, &stored); err != nil {
				t.Fatal(err)
			}
			stored.Schema++
			result, err := json.Marshal(stored)
			if err != nil {
				t.Fatal(err)
			}
			return result
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertCorruptArtifactMiss(t, test.mutate)
		})
	}
}

func assertCorruptArtifactMiss(t *testing.T, mutate func(*testing.T, []byte) []byte) {
	t.Helper()
	store := newTestStore(t)
	key := keyFor([]byte("unit"))
	ref, err := store.PutUnit(key, sampleUnit(key))
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.casPath("artifacts", ref.Digest)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mutated := mutate(t, data)
	mutatedRef := ArtifactRef{Digest: DigestBytes(mutated)}
	mutatedPath, _ := store.casPath("artifacts", mutatedRef.Digest)
	if err := writeAtomic(mutatedPath, mutated); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadUnit(mutatedRef, key); ok {
		t.Fatal("corrupt artifact was accepted")
	}
}

func TestWrongEmbeddedKeyIsCacheMiss(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	key := keyFor([]byte("unit"))
	ref, err := store.PutUnit(key, sampleUnit(key))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadUnit(ref, keyFor([]byte("other-unit"))); ok {
		t.Fatal("artifact was accepted for the wrong unit key")
	}

	wrong := sampleUnit(keyFor([]byte("embedded-other")))
	encoded, err := makeEnvelope("unit", unitSchemaVersion, string(key), wrong)
	if err != nil {
		t.Fatal(err)
	}
	forged := ArtifactRef{Digest: DigestBytes(encoded)}
	path, _ := store.casPath("artifacts", forged.Digest)
	if err := writeAtomic(path, encoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadUnit(forged, key); ok {
		t.Fatal("artifact with a wrong embedded key was accepted")
	}
}

func TestSourceTruncationIsCacheMiss(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	digest, err := store.PutSource([]byte("complete source"))
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.casPath("sources", digest)
	if err := os.Truncate(path, 3); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadSource(digest); ok {
		t.Fatal("truncated source was accepted")
	}
}

func TestConcurrentIdenticalSourcesShareOneCASBlob(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	contents := []byte("identical source contents")
	const writers = 32
	digests := make(chan Digest, writers)
	errors := make(chan error, writers)
	var group sync.WaitGroup
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			digest, err := store.PutSource(contents)
			if err != nil {
				errors <- err
				return
			}
			digests <- digest
		}()
	}
	group.Wait()
	close(digests)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	want := DigestBytes(contents)
	for digest := range digests {
		if digest != want {
			t.Fatalf("digest = %s, want %s", digest, want)
		}
	}
	loaded, ok := store.LoadSource(want)
	if !ok || !bytes.Equal(loaded, contents) {
		t.Fatalf("shared CAS blob = %q, %v", loaded, ok)
	}
}

func TestInterruptedCommitLeavesCurrentGeneration(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	workspace := keyFor([]byte("workspace"))
	projection := ArtifactRef{Digest: DigestBytes([]byte("projection"))}
	committed, err := store.CommitGeneration(workspace, Generation{Projection: projection})
	if err != nil {
		t.Fatal(err)
	}
	directory := store.workspaceDir(workspace)
	if err := os.WriteFile(filepath.Join(directory, ".tmp-interrupted-pointer"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "generations", ".tmp-interrupted-generation"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, ok := store.LoadGeneration(workspace)
	if !ok || !reflect.DeepEqual(loaded, committed) {
		t.Fatalf("interrupted commit displaced current generation: %#v, %v", loaded, ok)
	}
}

func TestConcurrentReadersObserveWholeGenerations(t *testing.T) {
	store := newTestStore(t)
	workspace := keyFor([]byte("workspace"))
	templates, wants := generationFixtures(40)
	if _, err := store.CommitGeneration(workspace, templates[0]); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	errCh := make(chan error, 16)
	var readers sync.WaitGroup
	for index := 0; index < 8; index++ {
		readers.Add(1)
		go readGenerationsUntilStopped(store, workspace, wants, stop, errCh, &readers)
	}
	for _, generation := range templates[1:] {
		if _, err := store.CommitGeneration(workspace, generation); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	readers.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	final, ok := store.LoadGeneration(workspace)
	if !ok || final.Number != uint64(len(templates)) {
		t.Fatalf("final generation = %#v, %v", final, ok)
	}
}

func generationFixtures(count int) ([]Generation, map[Digest]generationExpectation) {
	templates := make([]Generation, count)
	wants := make(map[Digest]generationExpectation, count)
	for index := range templates {
		projection := ArtifactRef{Digest: DigestBytes([]byte(fmt.Sprintf("projection-%d", index)))}
		key := keyFor([]byte(fmt.Sprintf("unit-%d", index)))
		ref := ArtifactRef{Digest: DigestBytes([]byte(fmt.Sprintf("artifact-%d", index)))}
		templates[index] = Generation{Projection: projection, Units: map[Key]ArtifactRef{key: ref}}
		wants[projection.Digest] = generationExpectation{key, ref}
	}
	return templates, wants
}

func readGenerationsUntilStopped(store *Store, workspace Key, wants map[Digest]generationExpectation, stop <-chan struct{}, errors chan<- error, done *sync.WaitGroup) {
	defer done.Done()
	for {
		select {
		case <-stop:
			return
		default:
		}
		generation, ok := store.LoadGeneration(workspace)
		if !ok {
			errors <- fmt.Errorf("current generation became a cache miss")
			return
		}
		want, known := wants[generation.Projection.Digest]
		if !known || len(generation.Units) != 1 || generation.Units[want.key] != want.ref {
			errors <- fmt.Errorf("observed torn generation: %#v", generation)
			return
		}
	}
}

func TestConcurrentWritersReceiveDistinctWholeGenerations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	firstStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	secondStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	stores := []*Store{firstStore, secondStore}
	workspace := keyFor([]byte("workspace"))
	const writerCount = 20
	numbers := make(chan uint64, writerCount)
	errors := make(chan error, writerCount)
	var writers sync.WaitGroup
	for index := 0; index < writerCount; index++ {
		index := index
		writers.Add(1)
		go func() {
			defer writers.Done()
			committed, err := stores[index%len(stores)].CommitGeneration(workspace, Generation{
				Projection: ArtifactRef{Digest: DigestBytes([]byte(fmt.Sprintf("p-%d", index)))},
			})
			if err != nil {
				errors <- err
				return
			}
			numbers <- committed.Number
		}()
	}
	writers.Wait()
	close(numbers)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	seen := make(map[uint64]bool, writerCount)
	for number := range numbers {
		if seen[number] {
			t.Fatalf("duplicate generation number %d", number)
		}
		seen[number] = true
	}
	for number := uint64(1); number <= writerCount; number++ {
		if !seen[number] {
			t.Fatalf("missing generation number %d", number)
		}
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func sampleUnit(key Key) UnitArtifact {
	passed := true
	return UnitArtifact{
		UnitID: "go:example/pkg", UnitKey: key, Language: "go", SnapshotKey: keyFor([]byte("snapshot")),
		Report: report.Document{
			SchemaVersion:  1,
			Diagnostics:    []map[string]any{{"code": "sample"}},
			ExecutionPlans: []map[string]any{{"parser_modes": []any{"syntax"}}},
			Files: []report.File{{
				Axes: map[string]float64{"complexity": 2}, Complete: true,
				Components: map[string]report.Component{"complexity": {
					Axis: "complexity", Contribution: 2, ObservedContribution: 2,
					Observations: 1, DeduplicatedObservations: 1,
					Subjects: []report.SubjectContribution{{Subject: "f", Value: 2, Contribution: 2}},
					Evidence: []report.MeasurementEvidence{{Name: "f", Value: 2, Scope: "routine"}},
					Waivers:  []map[string]any{{"reason": "none"}},
				}},
				Coverage: map[string]string{"complexity": "observed"}, Language: "go",
				ObservedAxes: map[string]float64{"complexity": 2}, ObservedScore: 2,
				Passed: &passed, Path: "pkg/a.go", Rank: 1, Score: 2,
			}},
		},
	}
}

func TestProjectionArtifactRoundTrip(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	workspace := keyFor([]byte("workspace"))
	projection := ProjectionFromReport(workspace, sampleUnit(keyFor([]byte("unit"))).Report, FreshnessCurrent)
	projection.GeneratedAt = time.Unix(123, 0).UTC()
	ref, err := store.PutProjection(workspace, projection)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := store.LoadProjection(ref, workspace)
	if !ok || !reflect.DeepEqual(got, projection) {
		t.Fatalf("LoadProjection() mismatch:\n got %#v\nwant %#v", got, projection)
	}
}

func BenchmarkPutAndLoadFiveThousandUnitArtifacts(b *testing.B) {
	store, err := NewStore(filepath.Join(b.TempDir(), "cache"))
	if err != nil {
		b.Fatal(err)
	}
	keys := make([]Key, 5000)
	for index := range keys {
		keys[index] = Key(DigestBytes([]byte(fmt.Sprintf("unit-%d", index))))
	}
	b.ResetTimer()
	for range b.N {
		refs := make([]ArtifactRef, len(keys))
		for index, key := range keys {
			refs[index], err = store.PutUnit(key, UnitArtifact{
				UnitID: fmt.Sprintf("unit-%d", index), UnitKey: key, Language: "go", SnapshotKey: key,
			})
			if err != nil {
				b.Fatal(err)
			}
		}
		for index, key := range keys {
			if _, ok := store.LoadUnit(refs[index], key); !ok {
				b.Fatalf("unit %d did not round-trip", index)
			}
		}
	}
}
