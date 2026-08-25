package analysiscache

import (
	"fmt"
	"os"
)

type sourceStore struct{ root string }

func (store *Store) PutSource(data []byte) (Digest, error) {
	return sourceStore{root: store.root}.put(data)
}

func (store *Store) LoadSource(digest Digest) ([]byte, bool) {
	return sourceStore{root: store.root}.load(digest)
}

func (store sourceStore) put(data []byte) (Digest, error) {
	digest := DigestBytes(data)
	path, ok := cachePath(store.root, "sources", digest)
	if !ok {
		return "", fmt.Errorf("invalid source digest")
	}
	if err := writeImmutable(path, data); err != nil {
		return "", fmt.Errorf("store source %s: %w", digest, err)
	}
	return digest, nil
}

func (store sourceStore) load(digest Digest) ([]byte, bool) {
	path, ok := cachePath(store.root, "sources", digest)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || DigestBytes(data) != digest {
		return nil, false
	}
	return data, true
}
