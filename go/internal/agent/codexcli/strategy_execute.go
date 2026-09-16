package codexcli

import (
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/agent"
)

func (strategy *Strategy) Execute(ctx context.Context, profile agent.Profile, request agent.Request, sink agent.EventSink) (result agent.Result) {
	result = agent.Result{JobID: request.JobID, AttemptID: request.AttemptID, Status: agent.ResultFailed}
	if err := ctx.Err(); err != nil {
		result.Status, result.Failure = agent.ResultCanceled, agent.FailureCancellation
		return result
	}
	executable, err := resolveExecutable(profile.Executable)
	if err != nil {
		result.Failure = agent.FailureUnavailable
		result.Diagnostic = err.Error()
		return result
	}
	root, err := canonicalAbsolute(request.Workspace.RepositoryRoot)
	if err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = "candidate root: " + err.Error()
		return result
	}
	policy, err := configuredProbePolicy(profile)
	if err != nil {
		result.Failure = agent.FailureInvalidProfile
		result.Diagnostic = err.Error()
		return result
	}
	run := newAppServerRun(request, sink)
	client, err := strategy.appServer(executable, root, request.Limits.MaxOutputBytes, run.handle)
	if err != nil {
		result.Failure = agent.FailureLaunch
		result.Diagnostic = err.Error()
		return result
	}
	defer func() {
		run.stop()
		if closeErr := client.Close(policy.terminationGrace); closeErr != nil {
			if result.Status == agent.ResultCompleted {
				result.Status, result.Failure = agent.ResultFailed, agent.FailureLaunch
			}
			if result.Diagnostic == "" {
				result.Diagnostic = sanitizeProviderText(closeErr.Error())
			}
		}
	}()
	probe := inspectAppServer(ctx, client)
	if probe.State != agent.ProbeReady || !probe.Capabilities.Isolation.EligibleForMutation() {
		if err := ctx.Err(); err != nil {
			return failedExecution(result, ctx, agent.FailureCancellation, err)
		}
		result.Failure, result.Diagnostic = failureForProbe(probe.State), probe.Diagnostic
		return result
	}
	model, modelOK := agent.ResolveOption(probe.Capabilities.Models, request.Model)
	effort, effortOK := agent.ResolveOption(probe.Capabilities.Efforts, request.Effort)
	if !modelOK || !effortOK {
		result.Failure = agent.FailureUnsupportedCapability
		result.Diagnostic = "requested Codex model or effort is unavailable"
		return result
	}
	request.Model, request.Effort = model, effort
	writableRoots, err := codexWritableRoots(root, request)
	if err != nil {
		return failedExecution(result, ctx, agent.FailureProtocol, err)
	}
	thread, err := startCodexThread(ctx, client, root, request)
	if err != nil {
		return failedExecution(result, ctx, agent.FailureProvider, err)
	}
	if thread == "" {
		return failedExecution(result, ctx, agent.FailureProtocol, errors.New("Codex thread/start returned no thread id"))
	}
	run.setThread(thread)
	turn, err := startCodexTurn(ctx, client, root, thread, request, writableRoots)
	if err != nil {
		return failedExecution(result, ctx, agent.FailureProvider, err)
	}
	if turn == "" {
		return failedExecution(result, ctx, agent.FailureProtocol, errors.New("Codex turn/start returned no turn id"))
	}
	run.setTurn(turn)
	return awaitCodexTurn(ctx, client, policy, run, thread, turn, strategy.reconcileEvery, result)
}

func codexWritableRoots(root string, request agent.Request) ([]string, error) {
	roots := []string{root}
	if request.Task.Manifest == nil {
		return roots, nil
	}
	manifestDirectory, err := targetManifestDirectory(request.Workspace, *request.Task.Manifest)
	if err != nil {
		return nil, err
	}
	return append(roots, manifestDirectory), nil
}

func startCodexThread(ctx context.Context, client *appServerClient, root string, request agent.Request) (string, error) {
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	err := client.Request(ctx, "thread/start", map[string]any{"cwd": root, "model": string(request.Model), "approvalPolicy": "never", "sandbox": "workspace-write", "ephemeral": true, "serviceName": "slopwatch"}, &response)
	return response.Thread.ID, err
}

func startCodexTurn(ctx context.Context, client *appServerClient, root, thread string, request agent.Request, writableRoots []string) (string, error) {
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	err := client.Request(ctx, "turn/start", map[string]any{"threadId": thread, "input": []map[string]any{{"type": "text", "text": request.Task.EffectivePrompt()}}, "cwd": root, "model": string(request.Model), "effort": string(request.Effort), "approvalPolicy": "never", "sandboxPolicy": map[string]any{"type": "workspaceWrite", "writableRoots": writableRoots, "networkAccess": false}}, &response)
	return response.Turn.ID, err
}
