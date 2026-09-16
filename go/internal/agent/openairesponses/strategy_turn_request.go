package openairesponses

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/blater/slopwatch/internal/agent"
)

// requestTurn serializes and submits one turn, enforcing outbound and
// response limits before returning provider data to the parser.
func (loop *responseTurnLoop) requestTurn() ([]byte, agent.Result, bool) {
	request := loop.request
	result := loop.result
	requestBody := apiRequest{
		Model: string(request.Model), Input: loop.history, Tools: functionTools(request.Task.Manifest != nil), ToolChoice: "auto", ParallelToolCalls: false,
		Reasoning: reasoningRequest{Effort: string(request.Effort)}, MaxOutputTokens: loop.config.maxOutputTokens,
		Store: false, Truncation: "disabled",
	}
	payload, err := json.Marshal(requestBody)
	if err != nil || int64(len(payload)) > loop.config.maxRequestBytes {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "Responses API request exceeded the configured context limit"
		return nil, result, true
	}
	// The authentication token belongs only in the Authorization header.
	// Enforce this at the adapter's final outbound boundary as well as at UI
	// admission, covering generated instructions and candidate tool output.
	if providerEchoesSecret(payload, loop.secret) {
		result.Failure = agent.FailureProtocol
		result.Diagnostic = "Responses API context contains protected authentication material"
		return nil, result, true
	}
	body, status, retryAfter, err := loop.strategy.post(loop.ctx, loop.config, loop.endpoint, loop.secret, payload, loop.remainingResponseBytes)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			result.Failure = agent.FailureProtocol
			result.Diagnostic = "Responses API exceeded the configured output limit"
			return nil, result, true
		}
		if errors.Is(err, errSecretEcho) {
			if status == http.StatusUnauthorized {
				result.Failure = agent.FailureUnauthenticated
				result.Diagnostic = "Responses API rejected authentication"
			} else {
				result.Failure = agent.FailureProtocol
				result.Diagnostic = "Responses API returned unsafe authentication material"
			}
			return nil, result, true
		}
		canceled := canceledOr(result, loop.ctx, agent.FailureProvider, "Responses API request failed")
		return nil, canceled, true
	}
	if status == http.StatusUnauthorized {
		result.Failure = agent.FailureUnauthenticated
		result.Diagnostic = "Responses API rejected authentication"
		return nil, result, true
	}
	if status == http.StatusForbidden {
		result.Failure = agent.FailureProvider
		result.Diagnostic = "Responses API request was forbidden; check the API key, organization/project, and model access"
		return nil, result, true
	}
	if status == http.StatusTooManyRequests {
		result.Failure = agent.FailureProvider
		result.Diagnostic = rateLimitDiagnostic(retryAfter)
		return nil, result, true
	}
	if status < 200 || status >= 300 {
		result.Failure = agent.FailureProvider
		result.Diagnostic = fmt.Sprintf("Responses API returned HTTP %d", status)
		return nil, result, true
	}
	return body, result, false
}
