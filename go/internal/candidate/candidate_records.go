package candidate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func writeOwnership(jobRoot string, record ownershipRecord) error {
	return writeCandidateRecord(jobRoot, ownershipName, record)
}

func ensureCandidateRecord(jobRoot, name string, record ownershipRecord) error {
	existing, err := readCandidateRecord(jobRoot, name)
	if err == nil {
		if !sameOwnership(existing, record) {
			return errors.New("existing candidate marker conflicts with admitted ownership")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeCandidateRecord(jobRoot, name, record)
}

func writeCandidateRecord(jobRoot, name string, record ownershipRecord) error {
	return writePrivateJSON(jobRoot, name, record)
}

func writeSeedManifest(jobRoot string, manifest seedManifest) (string, error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), writePrivateData(jobRoot, seedManifestName, data)
}

func writeSeedCompletion(jobRoot string, completion seedCompletion) error {
	return writePrivateJSON(jobRoot, seedCompletedName, completion)
}

func readSeedManifest(jobRoot string) (seedManifest, string, error) {
	data, err := readPrivateRegular(filepath.Join(jobRoot, seedManifestName))
	if err != nil {
		return seedManifest{}, "", err
	}
	var manifest seedManifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != 1 {
		return seedManifest{}, "", errors.New("candidate seed manifest is invalid")
	}
	for _, entry := range manifest.Entries {
		if entry.Size < 0 || entry.Size >= maxSeedByteLimit {
			return seedManifest{}, "", errors.New("candidate seed manifest has an invalid file size")
		}
	}
	hash := sha256.Sum256(data)
	return manifest, hex.EncodeToString(hash[:]), nil
}

func readSeedCompletion(jobRoot string) (seedCompletion, error) {
	data, err := readPrivateRegular(filepath.Join(jobRoot, seedCompletedName))
	if err != nil {
		return seedCompletion{}, err
	}
	var completion seedCompletion
	if err := json.Unmarshal(data, &completion); err != nil {
		return seedCompletion{}, err
	}
	return completion, nil
}

func writePrivateJSON(jobRoot, name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writePrivateData(jobRoot, name, data)
}

func writePrivateData(jobRoot, name string, data []byte) error {
	file, err := os.CreateTemp(jobRoot, "."+name+"-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, filepath.Join(jobRoot, name)); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(jobRoot)
}

func readOwnership(jobRoot string) (ownershipRecord, error) {
	return readCandidateRecord(jobRoot, ownershipName)
}

func readCandidateRecord(jobRoot, name string) (ownershipRecord, error) {
	path := filepath.Join(jobRoot, name)
	data, err := readPrivateRegular(path)
	if err != nil {
		return ownershipRecord{}, err
	}
	var record ownershipRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ownershipRecord{}, err
	}
	return record, nil
}
