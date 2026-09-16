package candidate

import (
	"strings"

	"github.com/blater/slopwatch/internal/fix"
)

func validJobID(job fix.JobID) bool {
	value := string(job)
	if !strings.HasPrefix(value, "job-") {
		return false
	}
	name := strings.TrimPrefix(value, "job-")
	words := strings.Split(name, "-")
	if len(words) != 3 {
		return false
	}
	for _, word := range words {
		if len(word) == 0 || len(word) > 8 {
			return false
		}
		for _, character := range word {
			if character < 'a' || character > 'z' {
				return false
			}
		}
	}
	return true
}
