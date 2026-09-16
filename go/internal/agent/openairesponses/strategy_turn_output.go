package openairesponses

import (
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/agent"
)

func (loop *responseTurnLoop) processResponse(body []byte) (agent.Result, bool) {
	response, output, err := decodeAPIResponse(body)
	if err != nil {
		return loop.protocolFailure(err.Error())
	}
	if response.Status != "completed" {
		loop.result.Failure = agent.FailureProvider
		loop.result.Diagnostic = "Responses API did not complete the model turn"
		return loop.result, true
	}
	if response.Usage != nil {
		if err := loop.recordUsage(response.Usage); err != nil {
			return loop.protocolFailure(err.Error())
		}
	}
	if output.refused {
		loop.result.Failure = agent.FailureProvider
		loop.result.Diagnostic = "Responses API refused the remediation request"
		loop.result.Usage = loop.usage
		return loop.result, true
	}
	if len(output.calls) == 0 {
		return loop.finishSummary(output.text)
	}
	loop.toolCalls += len(output.calls)
	if loop.config.maxToolCalls > 0 && loop.toolCalls > loop.config.maxToolCalls {
		loop.result.Failure = agent.FailureProtocol
		loop.result.Diagnostic = "Responses API exceeded the configured tool-call limit"
		return loop.result, true
	}
	// Validated output items are retained as model context. No provider-side
	// stored conversation or unbounded previous_response_id chain is used.
	loop.history = append(loop.history, response.Output...)
	if result, done := loop.runToolCalls(output.calls); done {
		return result, true
	}
	if contextSize(loop.history) > loop.config.maxContextBytes {
		loop.result.Failure = agent.FailureProtocol
		loop.result.Diagnostic = "agent tool loop exceeded the configured context limit"
		return loop.result, true
	}
	return loop.result, false
}

func (loop *responseTurnLoop) recordUsage(usage *apiUsage) error {
	loop.usage.InputTokens += usage.InputTokens
	loop.usage.OutputTokens += usage.OutputTokens
	if usage.InputTokenDetails != nil {
		loop.usage.CachedTokens += usage.InputTokenDetails.CachedTokens
	}
	if usage.OutputTokenDetails != nil {
		loop.usage.ReasoningTokens += usage.OutputTokenDetails.ReasoningTokens
	}
	loop.usage.Cumulative = true
	if loop.config.maxContextTokens > 0 && usage.InputTokens > loop.config.maxContextTokens {
		return errors.New("Responses API context exceeded the configured token limit")
	}
	copy := loop.usage
	if err := loop.emitter.emit(agent.EventUsage, "Token usage updated", "", primaryActorID, nil, &copy); err != nil {
		return errors.New("agent progress sink rejected a usage event")
	}
	return nil
}

func (loop *responseTurnLoop) finishSummary(text string) (agent.Result, bool) {
	if text == "" {
		return loop.protocolFailure("Responses API completed without a summary or tool call")
	}
	summary := boundedText(text, loop.config.maxSummaryBytes)
	if err := loop.emitter.emit(agent.EventRuntimeMessage, summary, "", primaryActorID, nil, nil); err != nil {
		return loop.protocolFailure("agent progress sink rejected the final message")
	}
	loop.result.Status = agent.ResultCompleted
	loop.result.Failure = agent.FailureNone
	loop.result.Summary = summary
	loop.result.Usage = loop.usage
	return loop.result, true
}

func (loop *responseTurnLoop) runToolCalls(calls []functionCall) (agent.Result, bool) {
	for _, call := range calls {
		if err := loop.emitter.emit(agent.EventCommandStarted, "Running "+call.Name, call.CallID, primaryActorID, nil, nil); err != nil {
			return loop.protocolFailure("agent progress sink rejected a tool event")
		}
		toolOutput, changed, toolErr := loop.tools.execute(loop.ctx, call)
		if toolErr != nil {
			if errors.Is(toolErr, context.Canceled) || errors.Is(toolErr, context.DeadlineExceeded) {
				loop.result = canceledOr(loop.result, loop.ctx, agent.FailureCancellation, "agent tool execution was canceled")
				return loop.result, true
			}
			return loop.protocolFailure("Responses API emitted an invalid tool call")
		}
		item, marshalErr := functionOutput(call.CallID, toolOutput)
		if marshalErr != nil {
			return loop.protocolFailure("agent tool result could not be encoded")
		}
		loop.history = append(loop.history, item)
		if changed != nil {
			if err := loop.emitter.emit(agent.EventFileChanged, "Candidate file changed", call.CallID, primaryActorID, changed, nil); err != nil {
				return loop.protocolFailure("agent progress sink rejected a file event")
			}
		}
		if err := loop.emitter.emit(agent.EventCommandFinished, "Finished "+call.Name, call.CallID, primaryActorID, nil, nil); err != nil {
			return loop.protocolFailure("agent progress sink rejected a tool event")
		}
	}
	return loop.result, false
}

func (loop *responseTurnLoop) protocolFailure(diagnostic string) (agent.Result, bool) {
	loop.result.Failure = agent.FailureProtocol
	loop.result.Diagnostic = diagnostic
	return loop.result, true
}
