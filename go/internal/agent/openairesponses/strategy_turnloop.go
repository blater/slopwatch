package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blater/slopwatch/internal/agent"
)

// responseTurnLoop owns state that spans one request/response/tool cycle.
// Keeping this state together preserves cancellation and cumulative limits.
type responseTurnLoop struct {
	strategy               *Strategy
	ctx                    context.Context
	config                 resolvedConfig
	endpoint, secret       string
	request                agent.Request
	emitter                *eventEmitter
	tools                  *candidateTools
	history                []json.RawMessage
	remainingResponseBytes int64
	result                 agent.Result
	usage                  agent.Usage
	toolCalls              int
}

func (loop *responseTurnLoop) run() agent.Result {
	for turn := 1; ; turn++ {
		result, done := loop.turn(turn)
		if done {
			return result
		}
	}
}

func (loop *responseTurnLoop) turn(turn int) (agent.Result, bool) {
	if loop.config.maxTurns > 0 && turn > loop.config.maxTurns {
		loop.result.Failure = agent.FailureProtocol
		loop.result.Diagnostic = "Responses API reached the configured model-turn budget"
		loop.result.Usage = loop.usage
		return loop.result, true
	}
	if err := loop.ctx.Err(); err != nil {
		loop.result = canceledOr(loop.result, loop.ctx, agent.FailureCancellation, "Responses API execution was canceled")
		return loop.result, true
	}
	if err := loop.emitter.emit(agent.EventActivity, fmt.Sprintf("Model turn %d", turn), "", primaryActorID, nil, nil); err != nil {
		loop.result.Failure = agent.FailureProtocol
		loop.result.Diagnostic = "agent progress sink rejected an activity event"
		return loop.result, true
	}
	body, result, done := loop.requestTurn()
	if done {
		loop.result = result
		return result, true
	}
	loop.remainingResponseBytes -= int64(len(body))
	return loop.processResponse(body)
}
