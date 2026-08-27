package fixapp

import (
	"context"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/fix"
)

func TestReconfigureExpandsAgentCapacityWithoutRestart(t *testing.T) {
	manager, runtime := newTestManager(t, 1)
	defer shutdownManager(t, manager)

	first := loadAndRun(t, manager, "one.go")
	second := loadAndRun(t, manager, "two.go")
	waitStarted(t, runtime, first)
	if job, _ := manager.Job(second); job.Phase != fix.PhaseQueued {
		t.Fatalf("second phase = %s, want queued", job.Phase)
	}

	if err := manager.Reconfigure(context.Background(), RuntimeLimits{MaxAgents: 2, MaxVerifiers: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case started := <-runtime.started:
		if started != second {
			t.Fatalf("started %s after reconfigure, want %s", started, second)
		}
	case <-time.After(time.Second):
		t.Fatal("queued job did not start after live capacity increase")
	}
	runtime.complete(first)
	runtime.complete(second)
}

func TestReconfigureReductionDoesNotCancelRunningJobsAndGatesNewScheduling(t *testing.T) {
	manager, runtime := newTestManager(t, 2)
	defer shutdownManager(t, manager)

	first := loadAndRun(t, manager, "one.go")
	second := loadAndRun(t, manager, "two.go")
	waitStarted(t, runtime, first, second)
	if err := manager.Reconfigure(context.Background(), RuntimeLimits{MaxAgents: 1, MaxVerifiers: 1}); err != nil {
		t.Fatal(err)
	}
	third := loadAndRun(t, manager, "three.go")
	runtime.complete(first)
	waitForPhase(t, manager, first, fix.PhaseCompleted)
	select {
	case started := <-runtime.started:
		t.Fatalf("job %s started while one pre-existing job still occupied reduced capacity", started)
	case <-time.After(50 * time.Millisecond):
	}
	if job, _ := manager.Job(second); job.Phase != fix.PhaseRunning {
		t.Fatalf("running job was disturbed by capacity reduction: %s", job.Phase)
	}
	runtime.complete(second)
	waitForPhase(t, manager, second, fix.PhaseCompleted)
	select {
	case started := <-runtime.started:
		if started != third {
			t.Fatalf("started %s, want queued job %s", started, third)
		}
	case <-time.After(time.Second):
		t.Fatal("queued job did not start after reduced-capacity slot became free")
	}
	runtime.complete(third)
}
