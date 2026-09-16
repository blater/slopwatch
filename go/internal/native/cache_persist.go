package native

import (
	"context"
	"runtime"
	"sync"

	"github.com/blater/slopwatch/internal/analysiscache"
)

type unitReportPersistenceError struct{ err error }

func (failure unitReportPersistenceError) Error() string { return failure.err.Error() }
func (failure unitReportPersistenceError) Unwrap() error { return failure.err }

func persistMissingUnits(ctx context.Context, store *analysiscache.Store, catalog catalogDocument, units []plannedCacheUnit, inputs map[string]scoreInputs, existing map[analysiscache.Key]analysiscache.ArtifactRef) (map[analysiscache.Key]analysiscache.ArtifactRef, error) {
	type persistResult struct {
		ref analysiscache.ArtifactRef
		err error
	}
	results := make([]persistResult, len(units))
	jobs := make(chan int)
	workers := min(runtime.GOMAXPROCS(0), 16)
	if workers < 1 {
		workers = 1
	}
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				unit := units[index]
				if _, exists := existing[unit.key]; exists {
					continue
				}
				if contextErr := ctx.Err(); contextErr != nil {
					results[index].err = contextErr
					continue
				}
				unitReport, reportErr := scoreInputsReport(catalog, []string{string(unit.plan.Language)}, inputs[unit.plan.ID], nil)
				if reportErr != nil {
					results[index].err = unitReportPersistenceError{err: reportErr}
					continue
				}
				results[index].ref, results[index].err = store.PutUnit(unit.key, analysiscache.UnitArtifact{
					UnitID: unit.plan.ID, UnitKey: unit.key, Language: string(unit.plan.Language),
					SnapshotKey: unit.key, Report: unitReport,
				})
			}
		}()
	}
	for index := range units {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	refs := make(map[analysiscache.Key]analysiscache.ArtifactRef)
	for index, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		if result.ref.Digest != "" {
			refs[units[index].key] = result.ref
		}
	}
	return refs, nil
}
