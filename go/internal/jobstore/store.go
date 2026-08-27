// Package jobstore persists one plain JSON state document per fix job.
package jobstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/blater/slopmochi/internal/fix"
)

var ErrJobRunning = errors.New("fix job is running in another Slopmochi process")

type Lock interface {
	Close() error
}

type Record struct {
	JobID     fix.JobID
	UpdatedAt time.Time
	State     json.RawMessage
}

type Store interface {
	Save(context.Context, Record) error
	Load(context.Context) ([]Record, error)
	Lock(fix.JobID) (Lock, error)
	Close() error
}
