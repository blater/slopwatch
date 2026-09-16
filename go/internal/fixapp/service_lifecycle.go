package fixapp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func New(dependencies Dependencies, options Options) (*Manager, error) {
	if dependencies.Config == nil || dependencies.Analysis == nil || dependencies.Candidates == nil || dependencies.Agents == nil || dependencies.Store == nil {
		return nil, errors.New("fix service dependencies are incomplete")
	}
	if options.MaxAgents <= 0 || options.MaxVerifiers <= 0 {
		return nil, errors.New("fix service requires explicit positive scheduler settings")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	records, err := dependencies.Store.Load(context.Background())
	if err != nil {
		_ = dependencies.Store.Close()
		return nil, fmt.Errorf("load fix job state: %w", err)
	}
	controller := &controller{controllerServices: controllerServices{deps: dependencies, options: options, initial: records},
		controllerRuntime: controllerRuntime{requests: make(chan any), events: make(chan agentUpdate, 256), results: make(chan workerResult, 32),
			done: make(chan struct{}), ready: make(chan struct{}), notify: make(chan struct{})},
		publication: publicationOwner{candidates: dependencies.Candidates, delivery: dependencies.Delivery,
			preflight: dependencies.DeliveryPreflight, publisher: dependencies.Publisher},
		persistence: persistenceOwner{store: dependencies.Store},
		logging:     loggingOwner{indexPath: options.JobIndexPath, clock: options.Clock},
		workers: workerOwner{agents: dependencies.Agents, candidates: dependencies.Candidates, analysis: dependencies.Analysis,
			clock: options.Clock, events: nil, results: nil, done: nil}, state: newControllerState()}
	controller.recovery = recoveryOwner{store: dependencies.Store, candidates: dependencies.Candidates,
		persistence: &controller.persistence, logging: &controller.logging, clock: options.Clock}
	manager := &Manager{controller: controller}
	controller.publication.results = controller.results
	controller.workers.events, controller.workers.results, controller.workers.done = controller.events, controller.results, controller.done
	go controller.run()
	<-controller.ready
	return manager, nil
}

func (manager *Manager) Reconfigure(ctx context.Context, limits RuntimeLimits) error {
	if manager.controller.closed.Load() {
		return ErrClosed
	}
	if limits.MaxAgents <= 0 || limits.MaxVerifiers <= 0 {
		return errors.New("fix runtime limits must all be greater than zero")
	}
	response := make(chan error, 1)
	if err := manager.send(ctx, reconfigureCall{ctx: ctx, limits: limits, response: response}); err != nil {
		return err
	}
	select {
	case err := <-response:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-manager.controller.done:
		return ErrClosed
	}
}
