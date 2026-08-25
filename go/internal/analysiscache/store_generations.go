package analysiscache

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type generationStore struct{ root string }

func (store *Store) CommitGeneration(view ViewKey, generation Generation) (Generation, error) {
	return generationStore{root: store.root}.commit(view, generation)
}

func (store *Store) LoadGeneration(view ViewKey) (Generation, bool) {
	return generationStore{root: store.root}.load(view)
}

func (store generationStore) commit(view ViewKey, generation Generation) (Generation, error) {
	if !validDigest(string(view)) {
		return Generation{}, fmt.Errorf("invalid workspace view key")
	}
	lock := workspaceLock(store.root, view)
	lock.Lock()
	defer lock.Unlock()
	directory := workspaceDir(store.root, view)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Generation{}, fmt.Errorf("create workspace cache directory: %w", err)
	}
	releaseFileLock, err := acquireFileLock(filepath.Join(directory, "writer.lock"))
	if err != nil {
		return Generation{}, fmt.Errorf("coordinate workspace cache writer: %w", err)
	}
	defer releaseFileLock()

	current, found := store.loadUnlocked(view)
	generation.ViewKey = view
	if found {
		generation.Number = current.Number + 1
	} else {
		generation.Number = 1
	}
	generation.CreatedAt = time.Now().UTC()
	if generation.Units == nil {
		generation.Units = map[Key]ArtifactRef{}
	}
	if !validDigest(string(generation.Projection.Digest)) {
		return Generation{}, fmt.Errorf("generation has invalid projection reference")
	}
	if err := validateUnitReferences(generation.Units); err != nil {
		return Generation{}, err
	}
	return store.publish(view, generation, directory)
}

func validateUnitReferences(units map[Key]ArtifactRef) error {
	for key, ref := range units {
		if !validDigest(string(key)) || !validDigest(string(ref.Digest)) {
			return fmt.Errorf("generation has invalid unit reference")
		}
	}
	return nil
}

func (store generationStore) publish(view ViewKey, generation Generation, directory string) (Generation, error) {
	encoded, err := makeEnvelope("manifest", manifestSchemaVersion, string(view), generation)
	if err != nil {
		return Generation{}, err
	}
	digest := DigestBytes(encoded)
	if err := os.MkdirAll(filepath.Join(directory, "generations"), 0o700); err != nil {
		return Generation{}, fmt.Errorf("create workspace generation directory: %w", err)
	}
	filename := fmt.Sprintf("%020d-%s.json", generation.Number, digest)
	generationPath := filepath.Join(directory, "generations", filename)
	if err := writeImmutable(generationPath, encoded); err != nil {
		return Generation{}, fmt.Errorf("write workspace generation: %w", err)
	}
	pointer := currentPointer{ViewKey: view, Generation: generation.Number, Filename: filename, Digest: digest}
	pointerBytes, err := makeEnvelope("pointer", manifestSchemaVersion, string(view), pointer)
	if err != nil {
		return Generation{}, err
	}
	if err := writeAtomic(filepath.Join(directory, "current.json"), pointerBytes); err != nil {
		return Generation{}, fmt.Errorf("commit workspace generation: %w", err)
	}
	return generation, nil
}

func (store generationStore) load(view ViewKey) (Generation, bool) {
	if !validDigest(string(view)) {
		return Generation{}, false
	}
	return store.loadUnlocked(view)
}

func (store generationStore) loadUnlocked(view ViewKey) (Generation, bool) {
	directory := workspaceDir(store.root, view)
	pointerData, err := os.ReadFile(filepath.Join(directory, "current.json"))
	if err != nil {
		return Generation{}, false
	}
	var pointer currentPointer
	if !decodeEnvelope(pointerData, "pointer", manifestSchemaVersion, string(view), &pointer) ||
		pointer.ViewKey != view || pointer.Filename != filepath.Base(pointer.Filename) ||
		!validDigest(string(pointer.Digest)) {
		return Generation{}, false
	}
	generationData, err := os.ReadFile(filepath.Join(directory, "generations", pointer.Filename))
	if err != nil || DigestBytes(generationData) != pointer.Digest {
		return Generation{}, false
	}
	var generation Generation
	if !decodeEnvelope(generationData, "manifest", manifestSchemaVersion, string(view), &generation) ||
		generation.ViewKey != view || generation.Number != pointer.Generation {
		return Generation{}, false
	}
	if !validDigest(string(generation.Projection.Digest)) {
		return Generation{}, false
	}
	if validateUnitReferences(generation.Units) != nil {
		return Generation{}, false
	}
	return generation, true
}
