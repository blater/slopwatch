package analysiscache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const envelopeMagic = "slopwatch-analysis-cache"

type envelope struct {
	Magic    string          `json:"magic"`
	Store    int             `json:"store_schema"`
	Kind     string          `json:"kind"`
	Schema   int             `json:"schema"`
	Key      string          `json:"key"`
	Checksum Digest          `json:"checksum"`
	Payload  json.RawMessage `json:"payload"`
}

type currentPointer struct {
	ViewKey    ViewKey `json:"view_key"`
	Generation uint64  `json:"generation"`
	Filename   string  `json:"filename"`
	Digest     Digest  `json:"digest"`
}

// Store owns a private, immutable content store and atomic workspace pointers.
// A Store is safe for concurrent readers and writers in one process.
type Store struct {
	root string
}

var processWorkspaceLocks sync.Map // map[canonical store root + ViewKey]*sync.Mutex

// DefaultRoot returns the per-user cache location without creating it.
// XDG_CACHE_HOME is honored only when it is an absolute, usable path;
// otherwise the cross-platform default is ~/.cache/slopwatch.
func DefaultRoot() (string, error) {
	xdg := os.Getenv("XDG_CACHE_HOME")
	if validAbsolutePath(xdg) {
		return cacheRoot(xdg, "")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home: %w", err)
	}
	return cacheRoot("", home)
}

func cacheRoot(xdg, home string) (string, error) {
	base := xdg
	if !validAbsolutePath(base) {
		if !validAbsolutePath(home) {
			return "", fmt.Errorf("user home is not an absolute path")
		}
		base = filepath.Join(filepath.Clean(home), ".cache")
	}
	return filepath.Join(filepath.Clean(base), "slopwatch"), nil
}

func validAbsolutePath(path string) bool {
	return strings.TrimSpace(path) != "" && filepath.IsAbs(path) && !strings.ContainsRune(path, '\x00')
}

// NewDefaultStore opens the default per-user cache.
func NewDefaultStore() (*Store, error) {
	root, err := DefaultRoot()
	if err != nil {
		return nil, err
	}
	return NewStore(root)
}

// NewStore creates a private cache hierarchy at root.
func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("cache root is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("canonicalize cache root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create cache root: %w", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve cache root: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("make cache root private: %w", err)
	}
	for _, name := range []string{"sources", "artifacts", "units", "workspaces"} {
		path := filepath.Join(absolute, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, fmt.Errorf("create cache directory %s: %w", name, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return nil, fmt.Errorf("make cache directory %s private: %w", name, err)
		}
	}
	return &Store{root: absolute}, nil
}

// Root reports the canonical cache directory.
func (store *Store) Root() string { return store.root }

// PutSource adds an immutable source blob and returns its content digest.
func (store *Store) PutSource(data []byte) (Digest, error) {
	digest := DigestBytes(data)
	path, ok := store.casPath("sources", digest)
	if !ok {
		return "", fmt.Errorf("invalid source digest")
	}
	if err := writeImmutable(path, data); err != nil {
		return "", fmt.Errorf("store source %s: %w", digest, err)
	}
	return digest, nil
}

// LoadSource returns a verified source blob. Missing, truncated, or corrupted
// data is an ordinary cache miss.
func (store *Store) LoadSource(digest Digest) ([]byte, bool) {
	path, ok := store.casPath("sources", digest)
	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || DigestBytes(data) != digest {
		return nil, false
	}
	return data, true
}

// PutUnit stores a lossless unit report. The supplied key is embedded in the
// artifact and checked again on every read.
func (store *Store) PutUnit(key Key, artifact UnitArtifact) (ArtifactRef, error) {
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
	if err := writeAtomic(store.unitIndexPath(key), pointer); err != nil {
		return ArtifactRef{}, fmt.Errorf("index unit artifact: %w", err)
	}
	return ref, nil
}

// LoadUnit returns a unit only when its envelope, schema, checksum, content
// address, and embedded unit key all validate.
func (store *Store) LoadUnit(ref ArtifactRef, expected Key) (UnitArtifact, bool) {
	var artifact UnitArtifact
	if !store.loadArtifact(ref, "unit", unitSchemaVersion, string(expected), &artifact) || artifact.UnitKey != expected {
		return UnitArtifact{}, false
	}
	return artifact, true
}

// LoadUnitByKey resolves the most recently published immutable artifact for a
// unit identity. A corrupt pointer or artifact is an ordinary cache miss.
func (store *Store) LoadUnitByKey(key Key) (UnitArtifact, ArtifactRef, bool) {
	if !validDigest(string(key)) {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	data, err := os.ReadFile(store.unitIndexPath(key))
	if err != nil {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	var ref ArtifactRef
	if !decodeEnvelope(data, "unit-pointer", unitSchemaVersion, string(key), &ref) || !validDigest(string(ref.Digest)) {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	artifact, ok := store.LoadUnit(ref, key)
	if !ok {
		return UnitArtifact{}, ArtifactRef{}, false
	}
	return artifact, ref, true
}

// PutProjection stores the compact workspace projection.
func (store *Store) PutProjection(view ViewKey, projection DisplayProjection) (ArtifactRef, error) {
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

// LoadProjection returns a verified compact projection.
func (store *Store) LoadProjection(ref ArtifactRef, view ViewKey) (DisplayProjection, bool) {
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

// CommitGeneration atomically installs a complete workspace view. Number and
// CreatedAt are assigned while holding the per-workspace writer lock. Previous
// immutable generation files remain valid if a commit is interrupted.
func (store *Store) CommitGeneration(view ViewKey, generation Generation) (Generation, error) {
	if !validDigest(string(view)) {
		return Generation{}, fmt.Errorf("invalid workspace view key")
	}
	lock := store.workspaceLock(view)
	lock.Lock()
	defer lock.Unlock()
	directory := store.workspaceDir(view)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Generation{}, fmt.Errorf("create workspace cache directory: %w", err)
	}
	releaseFileLock, err := acquireFileLock(filepath.Join(directory, "writer.lock"))
	if err != nil {
		return Generation{}, fmt.Errorf("coordinate workspace cache writer: %w", err)
	}
	defer releaseFileLock()

	current, found := store.loadGenerationUnlocked(view)
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
	for key, ref := range generation.Units {
		if !validDigest(string(key)) || !validDigest(string(ref.Digest)) {
			return Generation{}, fmt.Errorf("generation has invalid unit reference")
		}
	}

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

// LoadGeneration returns the last wholly committed generation. Invalid current
// pointers or generation files are treated as misses; temp files are ignored.
func (store *Store) LoadGeneration(view ViewKey) (Generation, bool) {
	if !validDigest(string(view)) {
		return Generation{}, false
	}
	return store.loadGenerationUnlocked(view)
}

func (store *Store) loadGenerationUnlocked(view ViewKey) (Generation, bool) {
	directory := store.workspaceDir(view)
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
	for key, ref := range generation.Units {
		if !validDigest(string(key)) || !validDigest(string(ref.Digest)) {
			return Generation{}, false
		}
	}
	return generation, true
}

func (store *Store) putArtifact(kind string, schema int, key string, value any) (ArtifactRef, error) {
	encoded, err := makeEnvelope(kind, schema, key, value)
	if err != nil {
		return ArtifactRef{}, err
	}
	digest := DigestBytes(encoded)
	path, _ := store.casPath("artifacts", digest)
	if err := writeImmutable(path, encoded); err != nil {
		return ArtifactRef{}, fmt.Errorf("store %s artifact: %w", kind, err)
	}
	return ArtifactRef{Digest: digest}, nil
}

func (store *Store) loadArtifact(ref ArtifactRef, kind string, schema int, key string, target any) bool {
	path, ok := store.casPath("artifacts", ref.Digest)
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

func (store *Store) casPath(namespace string, digest Digest) (string, bool) {
	value := string(digest)
	if !validDigest(value) {
		return "", false
	}
	return filepath.Join(store.root, namespace, value[:2], value[2:]), true
}

func (store *Store) workspaceDir(key Key) string {
	return filepath.Join(store.root, "workspaces", string(key))
}

func (store *Store) unitIndexPath(key Key) string {
	value := string(key)
	return filepath.Join(store.root, "units", value[:2], value[2:]+".json")
}

func (store *Store) workspaceLock(key Key) *sync.Mutex {
	identity := store.root + "\x00" + string(key)
	value, _ := processWorkspaceLocks.LoadOrStore(identity, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func writeImmutable(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("immutable cache path contains different data")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomicNoReplace(path, data)
}

func writeAtomic(path string, data []byte) error {
	return writeAtomicMode(path, data, false)
}

func writeAtomicNoReplace(path string, data []byte) error {
	return writeAtomicMode(path, data, true)
}

func writeAtomicMode(path string, data []byte, noReplace bool) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".tmp-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if noReplace {
		if existing, err := os.ReadFile(path); err == nil {
			if !bytes.Equal(existing, data) {
				return fmt.Errorf("immutable cache path contains different data")
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(temporaryName, path); err != nil {
		if noReplace {
			if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
				return nil
			}
		}
		return err
	}
	return syncDirectory(directory)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
