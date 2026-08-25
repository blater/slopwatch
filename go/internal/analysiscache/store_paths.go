package analysiscache

import (
	"path/filepath"
	"sync"
)

func (store *Store) casPath(namespace string, digest Digest) (string, bool) {
	return cachePath(store.root, namespace, digest)
}

func cachePath(root, namespace string, digest Digest) (string, bool) {
	value := string(digest)
	if !validDigest(value) {
		return "", false
	}
	return filepath.Join(root, namespace, value[:2], value[2:]), true
}

func (store *Store) workspaceDir(key Key) string {
	return workspaceDir(store.root, key)
}

func workspaceDir(root string, key Key) string {
	return filepath.Join(root, "workspaces", string(key))
}

func (store *Store) unitIndexPath(key Key) string {
	return unitIndexPath(store.root, key)
}

func unitIndexPath(root string, key Key) string {
	value := string(key)
	return filepath.Join(root, "units", value[:2], value[2:]+".json")
}

func workspaceLock(root string, key Key) *sync.Mutex {
	identity := root + "\x00" + string(key)
	value, _ := processWorkspaceLocks.LoadOrStore(identity, &sync.Mutex{})
	return value.(*sync.Mutex)
}
