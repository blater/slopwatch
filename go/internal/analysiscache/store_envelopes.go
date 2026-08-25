package analysiscache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

func (store artifactStore) putArtifact(kind string, schema int, key string, value any) (ArtifactRef, error) {
	encoded, err := makeEnvelope(kind, schema, key, value)
	if err != nil {
		return ArtifactRef{}, err
	}
	digest := DigestBytes(encoded)
	path, _ := cachePath(store.root, "artifacts", digest)
	if err := writeImmutable(path, encoded); err != nil {
		return ArtifactRef{}, fmt.Errorf("store %s artifact: %w", kind, err)
	}
	return ArtifactRef{Digest: digest}, nil
}

func (store artifactStore) loadArtifact(ref ArtifactRef, kind string, schema int, key string, target any) bool {
	path, ok := cachePath(store.root, "artifacts", ref.Digest)
	if !ok {
		return false
	}
	encoded, err := os.ReadFile(path)
	if err != nil || DigestBytes(encoded) != ref.Digest {
		return false
	}
	return decodeEnvelope(encoded, kind, schema, key, target)
}

func makeEnvelope(kind string, schema int, key string, value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s cache payload: %w", kind, err)
	}
	encoded, err := json.Marshal(envelope{
		Magic: envelopeMagic, Store: storeSchemaVersion, Kind: kind,
		Schema: schema, Key: key, Checksum: DigestBytes(payload), Payload: payload,
	})
	if err != nil {
		return nil, fmt.Errorf("encode %s cache envelope: %w", kind, err)
	}
	return append(encoded, '\n'), nil
}

func decodeEnvelope(data []byte, kind string, schema int, key string, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var stored envelope
	if decoder.Decode(&stored) != nil || stored.Magic != envelopeMagic ||
		stored.Store != storeSchemaVersion || stored.Kind != kind ||
		stored.Schema != schema || stored.Key != key ||
		DigestBytes(stored.Payload) != stored.Checksum {
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return false
	}
	return json.Unmarshal(stored.Payload, target) == nil
}
