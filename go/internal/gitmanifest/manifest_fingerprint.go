package gitmanifest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
)

func fingerprint(entries []Entry) string {
	hasher := sha256.New()
	for _, entry := range entries {
		writeField(hasher, entry.Status)
		writeField(hasher, entry.Path.String())
		writeField(hasher, entry.Previous.String())
		writeField(hasher, entry.Kind)
		var mode [4]byte
		binary.BigEndian.PutUint32(mode[:], entry.Mode)
		_, _ = hasher.Write(mode[:])
		writeField(hasher, entry.Hash)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func writeField(writer io.Writer, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = io.WriteString(writer, value)
}
