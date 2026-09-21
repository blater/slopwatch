package analysiscache

import (
	"fmt"
	"os"
)

type artifactStore struct{ root string }

func (store *Store) PutUnit(key Key, artifact UnitArtifact) (ArtifactRef, error) {
	return artifactStore{root: store.root}.putUnit(key, artifact)
}

func (store *Store) LoadUnit(ref ArtifactRef) (UnitArtifact, bool) {
	return artifactStore{root: store.root}.loadUnit(ref)
}

func (store *Store) LoadUnitByKey(key Key) (UnitArtifact, ArtifactRef, bool) {
	return artifactStore{root: store.root}.loadUnitByKey(key)
}

func (store *Store) PutProjection(view ViewKey, projection DisplayProjection) (ArtifactRef, error) {
	return artifactStore{root: store.root}.putProjection(view, projection)
}

func (store *Store) LoadProjection(ref ArtifactRef) (DisplayProjection, bool) {
	return artifactStore{root: store.root}.loadProjection(ref)
}

func (store artifactStore) putUnit(key Key, artifact UnitArtifact) (ArtifactRef, error) {
	if !validDigest(string(key)) {
		return ArtifactRef{}, fmt.Errorf("invalid unit key")
	}
	artifact.UnitKey = key
	ref, err := store.putArtifact("unit", artifact)
	if err != nil {
		return ArtifactRef{}, err
	}
	pointer, err := makeEnvelope(ref)
	if err != nil {
		return ArtifactRef{}, err
	}
	if err := writeAtomic(unitIndexPath(store.root, key), pointer); err != nil {
		return ArtifactRef{}, fmt.Errorf("index unit artifact: %w", err)
	}
	return ref, nil
}

func (store artifactStore) loadUnit(ref ArtifactRef) (UnitArtifact, bool) {
	var artifact UnitArtifact
	if !store.loadArtifact(ref, &artifact) {
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
	if !decodeEnvelope(data, &ref) || !validDigest(string(ref.Digest)) {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	artifact, ok := store.loadUnit(ref)
	if !ok {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	return artifact, ref, true
}

func (store artifactStore) putProjection(view ViewKey, projection DisplayProjection) (ArtifactRef, error) {
	if !validDigest(string(view)) {
		return ArtifactRef{}, fmt.Errorf("invalid workspace view key")
	}
	for _, file := range projection.Files {
		if err := validateFreshness(file.Freshness); err != nil {
			return ArtifactRef{}, err
		}
	}
	projection.ViewKey = view
	return store.putArtifact("projection", projection)
}

func (store artifactStore) loadProjection(ref ArtifactRef) (DisplayProjection, bool) {
	var projection DisplayProjection
	if !store.loadArtifact(ref, &projection) {
		return DisplayProjection{}, false
	}
	for _, file := range projection.Files {
		if validateFreshness(file.Freshness) != nil {
			return DisplayProjection{}, false
		}
	}
	return projection, true
}
