package analysiscache

import (
	"fmt"
	"os"
)

type artifactStore struct{ root string }

func (store *Store) PutUnit(key Key, artifact UnitArtifact) (ArtifactRef, error) {
	return artifactStore{root: store.root}.putUnit(key, artifact)
}

func (store *Store) LoadUnit(ref ArtifactRef, expected Key) (UnitArtifact, bool) {
	return artifactStore{root: store.root}.loadUnit(ref, expected)
}

func (store *Store) LoadUnitByKey(key Key) (UnitArtifact, ArtifactRef, bool) {
	return artifactStore{root: store.root}.loadUnitByKey(key)
}

func (store *Store) PutProjection(view ViewKey, projection DisplayProjection) (ArtifactRef, error) {
	return artifactStore{root: store.root}.putProjection(view, projection)
}

func (store *Store) LoadProjection(ref ArtifactRef, view ViewKey) (DisplayProjection, bool) {
	return artifactStore{root: store.root}.loadProjection(ref, view)
}

func (store artifactStore) putUnit(key Key, artifact UnitArtifact) (ArtifactRef, error) {
	if !validDigest(string(key)) {
		return ArtifactRef{}, fmt.Errorf("invalid unit key")
	}
	if artifact.UnitKey != "" && artifact.UnitKey != key {
		return ArtifactRef{}, fmt.Errorf("unit artifact key does not match cache key")
	}
	artifact.UnitKey = key
	ref, err := store.putArtifact("unit", unitSchemaVersion, string(key), artifact)
	if err != nil {
		return ArtifactRef{}, err
	}
	pointer, err := makeEnvelope("unit-pointer", unitSchemaVersion, string(key), ref)
	if err != nil {
		return ArtifactRef{}, err
	}
	if err := writeAtomic(unitIndexPath(store.root, key), pointer); err != nil {
		return ArtifactRef{}, fmt.Errorf("index unit artifact: %w", err)
	}
	return ref, nil
}

func (store artifactStore) loadUnit(ref ArtifactRef, expected Key) (UnitArtifact, bool) {
	var artifact UnitArtifact
	if !store.loadArtifact(ref, "unit", unitSchemaVersion, string(expected), &artifact) || artifact.UnitKey != expected {
		return UnitArtifact{}, false
	}
	return artifact, true
}

func (store artifactStore) loadUnitByKey(key Key) (UnitArtifact, ArtifactRef, bool) {
	if !validDigest(string(key)) {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	data, err := os.ReadFile(unitIndexPath(store.root, key))
	if err != nil {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	var ref ArtifactRef
	if !decodeEnvelope(data, "unit-pointer", unitSchemaVersion, string(key), &ref) || !validDigest(string(ref.Digest)) {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	artifact, ok := store.loadUnit(ref, key)
	if !ok {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	return artifact, ref, true
}

func (store artifactStore) putProjection(view ViewKey, projection DisplayProjection) (ArtifactRef, error) {
	if !validDigest(string(view)) {
		return ArtifactRef{}, fmt.Errorf("invalid workspace view key")
	}
	if projection.ViewKey != "" && projection.ViewKey != view {
		return ArtifactRef{}, fmt.Errorf("projection workspace view key does not match")
	}
	for _, file := range projection.Files {
		if err := validateFreshness(file.Freshness); err != nil {
			return ArtifactRef{}, err
		}
	}
	projection.ViewKey = view
	return store.putArtifact("projection", projectionSchemaVersion, string(view), projection)
}

func (store artifactStore) loadProjection(ref ArtifactRef, view ViewKey) (DisplayProjection, bool) {
	var projection DisplayProjection
	if !store.loadArtifact(ref, "projection", projectionSchemaVersion, string(view), &projection) || projection.ViewKey != view {
		return DisplayProjection{}, false
	}
	for _, file := range projection.Files {
		if validateFreshness(file.Freshness) != nil {
			return DisplayProjection{}, false
		}
	}
	return projection, true
}
