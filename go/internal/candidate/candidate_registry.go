package candidate

import (
	"errors"
	"sync"

	"github.com/blater/slopwatch/internal/fix"
)

// candidateRegistry owns the synchronized in-memory half of durable candidate
// ownership. Filesystem markers remain the source of truth across restarts.
type candidateRegistry struct {
	mu        sync.Mutex
	policies  map[fix.JobID]candidatePolicy
	leases    map[string]*repositoryOwnership
	jobLeases map[fix.JobID]string
	closed    bool
}

func newCandidateRegistry() candidateRegistry {
	return candidateRegistry{policies: map[fix.JobID]candidatePolicy{}, leases: map[string]*repositoryOwnership{}, jobLeases: map[fix.JobID]string{}}
}

func (registry *candidateRegistry) setPolicy(job fix.JobID, policy candidatePolicy) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.policies[job] = policy
}

func (registry *candidateRegistry) deletePolicy(job fix.JobID) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(registry.policies, job)
}

func (registry *candidateRegistry) leaseFor(job fix.JobID) (string, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	common, ok := registry.jobLeases[job]
	return common, ok
}

func (registry *candidateRegistry) retain(common string, job fix.JobID) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.closed {
		return errors.New("candidate service is closed")
	}
	if existing, ok := registry.jobLeases[job]; ok {
		if existing != common {
			return errors.New("job already owns a different repository lease")
		}
		return nil
	}
	owned := registry.leases[common]
	if owned == nil {
		lease, err := acquireRepositoryOwnership(common)
		if err != nil {
			return err
		}
		owned = &repositoryOwnership{lease: lease, jobs: map[fix.JobID]bool{}}
		registry.leases[common] = owned
	}
	owned.jobs[job] = true
	registry.jobLeases[job] = common
	return nil
}

func (registry *candidateRegistry) release(common string, job fix.JobID) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.jobLeases[job] != common {
		return nil
	}
	delete(registry.jobLeases, job)
	owned := registry.leases[common]
	if owned == nil {
		return nil
	}
	delete(owned.jobs, job)
	if len(owned.jobs) != 0 {
		return nil
	}
	delete(registry.leases, common)
	return owned.lease.Close()
}

func (registry *candidateRegistry) close() error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.closed {
		return nil
	}
	registry.closed = true
	var result error
	for common, owned := range registry.leases {
		result = errors.Join(result, owned.lease.Close())
		delete(registry.leases, common)
	}
	clear(registry.jobLeases)
	return result
}
