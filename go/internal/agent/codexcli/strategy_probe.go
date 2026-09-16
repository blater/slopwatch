package codexcli

import (
	"context"

	"github.com/blater/slopwatch/internal/agent"
)

func (strategy *Strategy) Probe(parent context.Context, profile agent.Profile) agent.ProbeResult {
	result := agent.ProbeResult{Runtime: RuntimeKind, State: agent.ProbeUnavailable}
	if strategy == nil {
		result.Diagnostic = "Codex App Server adapter is not configured"
		return result
	}
	policy, err := configuredProbePolicy(profile)
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	executable, err := resolveExecutable(profile.Executable)
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	directory, err := strategy.workingDirectory()
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	ctx, cancel := context.WithTimeout(parent, policy.timeout)
	defer cancel()
	client, err := strategy.appServer(executable, directory, diagnosticCaptureLimit, nil)
	if err != nil {
		result.Diagnostic = err.Error()
		return result
	}
	result = inspectAppServer(ctx, client)
	if closeErr := client.Close(policy.terminationGrace); closeErr != nil {
		result.State = agent.ProbeUnavailable
		result.Diagnostic = sanitizeProviderText(closeErr.Error())
	}
	return result
}
