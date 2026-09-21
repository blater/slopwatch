package analysiscache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

func (store artifactStore) putArtifact(kind string, value any) (ArtifactRef, error) {
	encoded, err := makeEnvelope(value)
	if err != nil {
		return ArtifactRef{}, err
	}
	digest := DigestBytes(encoded)
	path, _ := cachePath(store.root, "artifacts", digest)
	stored, err := encodeArtifactStorage(encoded)
	if err != nil {
		return ArtifactRef{}, err
	}
	if err := writeArtifact(path, stored); err != nil {
		return ArtifactRef{}, fmt.Errorf("store %s artifact: %w", kind, err)
	}
	return ArtifactRef{Digest: digest}, nil
}

func (store artifactStore) loadArtifact(ref ArtifactRef, target any) bool {
	path, ok := cachePath(store.root, "artifacts", ref.Digest)
	if !ok {
		return false
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	encoded, err = decodeArtifactStorage(encoded)
	if err != nil {
		return false
	}
	return decodeEnvelope(encoded, target)
}

func makeEnvelope(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode cache payload: %w", err)
	}
	encoded, err := json.Marshal(envelope{Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("encode cache envelope: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeEnvelope(data []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var stored envelope
	if decoder.Decode(&stored) != nil {
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return false
	}
	return json.Unmarshal(stored.Payload, target) == nil
}
