package native

import (
	"context"
	"runtime"
	"sync"

	"github.com/blater/slopwatch/internal/analysiscache"
)

type cachedUnitLoad struct {
	artifact analysiscache.UnitArtifact
	ref      analysiscache.ArtifactRef
	loaded   bool
}

func loadCachedUnits(ctx context.Context, store *analysiscache.Store, generation analysiscache.Generation, units []plannedCacheUnit, enabled bool) []cachedUnitLoad {
	loads := make([]cachedUnitLoad, len(units))
	if !enabled || len(units) == 0 {
		return loads
	}
	workers := min(runtime.GOMAXPROCS(0), 16)
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				unit := units[index]
				ref, candidate := generation.Units[unit.key]
				artifact := analysiscache.UnitArtifact{}
				loaded := false
				if candidate {
					artifact, loaded = store.LoadUnit(ref, unit.key)
				}
				if !loaded {
					artifact, ref, loaded = store.LoadUnitByKey(unit.key)
				}
				loads[index] = cachedUnitLoad{artifact: artifact, ref: ref, loaded: loaded}
			}
		}()
	}
	for index := range units {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	return loads
}
