package gitmanifest

import (
	"bytes"
	"errors"
)

type statusEntry struct{ status, path, previous string }

func parse(data []byte) ([]statusEntry, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != 0 {
		return nil, errors.New("Git porcelain status is not NUL terminated")
	}
	parts := bytes.Split(data, []byte{0})
	result := make([]statusEntry, 0, len(parts)-1)
	for index := 0; index < len(parts)-1; index++ {
		part := parts[index]
		if len(part) < 4 || part[2] != ' ' || part[0] == ' ' && part[1] == ' ' {
			return nil, errors.New("malformed Git porcelain status")
		}
		item := statusEntry{status: string(part[:2]), path: string(part[3:])}
		if item.path == "" {
			return nil, errors.New("empty Git status path")
		}
		if isRename(item.status) {
			index++
			if index >= len(parts)-1 || len(parts[index]) == 0 {
				return nil, errors.New("malformed Git rename status")
			}
			item.previous = string(parts[index])
		}
		result = append(result, item)
	}
	return result, nil
}

func isRename(status string) bool {
	return len(status) == 2 && (status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C')
}

func isDeleted(status string) bool {
	return len(status) == 2 && (status[0] == 'D' || status[1] == 'D')
}
