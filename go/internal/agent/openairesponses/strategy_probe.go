package openairesponses

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/blater/slopwatch/internal/agent"
)

type probeHTTPResponse struct {
	status int
	body   []byte
	retry  string
}

var errProbeUnavailable = errors.New("Responses API could not be reached")

// fetchProbe owns the bounded catalog request. Probe keeps the state-machine
// decisions in the public method while this helper owns HTTP cleanup/limits.
func (strategy *Strategy) fetchProbe(ctx context.Context, config resolvedConfig, endpoint, secret string) (probeHTTPResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsEndpoint(endpoint), nil)
	if err != nil {
		return probeHTTPResponse{}, errors.New("Responses API endpoint is invalid")
	}
	setHeaders(request, secret, false)
	response, err := strategy.config.client.Do(request)
	if err != nil {
		return probeHTTPResponse{}, errProbeUnavailable
	}
	defer response.Body.Close()
	body, tooLarge, readErr := readBounded(response.Body, config.maxProbeBytes)
	if readErr != nil || tooLarge {
		return probeHTTPResponse{}, errors.New("Responses API probe returned an invalid response")
	}
	return probeHTTPResponse{status: response.StatusCode, body: body, retry: response.Header.Get("Retry-After")}, nil
}

func finishProbe(result agent.ProbeResult, response probeHTTPResponse, secret string, configured []agent.Option[agent.ModelID]) agent.ProbeResult {
	if providerEchoesSecret(response.body, secret) {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Responses API returned unsafe authentication material"
		return result
	}
	switch response.status {
	case http.StatusUnauthorized:
		result.State = agent.ProbeUnauthenticated
		result.Diagnostic = "Responses API rejected authentication"
		return result
	case http.StatusForbidden:
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Responses API request was forbidden; check the API key, organization/project, and model access"
		return result
	case http.StatusTooManyRequests:
		result.Diagnostic = rateLimitDiagnostic(response.retry)
		return result
	}
	if response.status < 200 || response.status >= 300 {
		result.Diagnostic = fmt.Sprintf("Responses API probe returned HTTP %d", response.status)
		return result
	}
	available, err := availableConfiguredModels(response.body, configured)
	if err != nil {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "Responses API model catalog was invalid"
		return result
	}
	if len(available) == 0 {
		result.State = agent.ProbeIncompatible
		result.Diagnostic = "None of this profile's configured models are available to the account"
		result.Capabilities.Models = nil
		return result
	}
	result.Capabilities.Models = available
	result.State = agent.ProbeReady
	result.Version = "responses-v1"
	result.Authentication = agent.Authentication{Method: "api-key", Label: "API key available (usage billed separately)"}
	return result
}
