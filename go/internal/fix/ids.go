// Package fix contains immutable, provider-neutral domain values for
// agent-assisted remediation jobs.
package fix

import "github.com/blater/slopwatch/internal/naming"

type JobID string
type AttemptID string
type CommandID string
type DraftID string
type RepositoryID string
type ObjectID string

func NewJobID() (JobID, error)         { value, err := newID("job"); return JobID(value), err }
func NewAttemptID() (AttemptID, error) { value, err := newID("attempt"); return AttemptID(value), err }
func NewCommandID() (CommandID, error) { value, err := newID("command"); return CommandID(value), err }
func NewDraftID() (DraftID, error)     { value, err := newID("draft"); return DraftID(value), err }

func newID(prefix string) (string, error) {
	return naming.New(prefix)
}
