package codexcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/agent"
)

func awaitCodexTurn(ctx context.Context, client *appServerClient, policy probePolicy, run *appServerRun, thread, turn string, interval time.Duration, result agent.Result) agent.Result {
	if interval <= 0 {
		interval = defaultReconcileEvery
	}
	reconcile := time.NewTicker(interval)
	defer reconcile.Stop()
	reconcileCtx, stopReconciliation := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { stopReconciliation(); workers.Wait() }()
	reconciled := make(chan turnReconciliation, 1)
	inFlight := false
	for {
		select {
		case completed := <-run.completed:
			return completedExecution(result, ctx, run, thread, completed)
		case <-reconcile.C:
			if !run.hasFinalAnswer() || inFlight {
				continue
			}
			inFlight = true
			workers.Add(1)
			go func() {
				defer workers.Done()
				completed, terminal, reconcileErr := reconcileCodexTurn(reconcileCtx, client, thread)
				reconciled <- turnReconciliation{completed: completed, terminal: terminal, err: reconcileErr}
			}()
		case reconciliation := <-reconciled:
			inFlight = false
			if reconciliation.err != nil {
				if errors.Is(reconciliation.err, context.Canceled) && reconcileCtx.Err() != nil {
					continue
				}
				run.setError(fmt.Errorf("reconcile Codex completion: %w", reconciliation.err))
			} else if reconciliation.terminal {
				run.complete(reconciliation.completed)
			} else {
				continue
			}
			completed := <-run.completed
			return completedExecution(result, ctx, run, thread, completed)
		case <-ctx.Done():
			return cancelCodexTurn(ctx, client, policy, run, thread, turn, result)
		case <-client.done:
			select {
			case completed := <-run.completed:
				return completedExecution(result, ctx, run, thread, completed)
			default:
			}
			exitErr := client.protocolExitError()
			failure := agent.FailureProvider
			if strings.Contains(exitErr.Error(), "configured") || strings.Contains(exitErr.Error(), "decode Codex App Server") {
				failure = agent.FailureProtocol
			}
			return failedExecution(result, ctx, failure, exitErr)
		}
	}
}

func cancelCodexTurn(ctx context.Context, client *appServerClient, policy probePolicy, run *appServerRun, thread, turn string, result agent.Result) agent.Result {
	cancelCtx, cancel := context.WithTimeout(context.Background(), policy.terminationGrace)
	_ = client.Request(cancelCtx, "turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, nil)
	select {
	case <-run.completed:
	case <-cancelCtx.Done():
	}
	cancel()
	result.Status, result.Failure = agent.ResultCanceled, agent.FailureCancellation
	result.SessionReference = thread
	result.Summary, result.Usage = run.snapshot()
	return result
}
