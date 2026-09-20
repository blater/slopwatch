package native

import (
	"context"
	"errors"
	"fmt"
)

var errMissingTerminal = errors.New("analyzer protocol ended without terminal status")

// A terminal failure describes a validated stream, unlike a malformed protocol.
type analyzerTerminalFailure struct{ status, message string }

func (failure *analyzerTerminalFailure) Error() string {
	return fmt.Sprintf("analyzer terminal status %s: %s", failure.status, failure.message)
}

type analyzerProtocolError struct{ error }

func (failure *analyzerProtocolError) Unwrap() error { return failure.error }
func isTerminalFailure(err error) bool {
	var failure *analyzerTerminalFailure
	return errors.As(err, &failure)
}
func fatalAnalyzerError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var protocol *analyzerProtocolError
	if errors.As(err, &protocol) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
func terminalDiagnostic(record protocolRecord, err error) protocolRecord {
	return protocolRecord{Type: "diagnostic", Raw: map[string]any{
		"type": "diagnostic", "protocol_version": record.Version,
		"invocation_id": record.Invocation, "severity": "error",
		"code": "native.terminal_failure", "message": err.Error(),
	}}
}

func recoverAnalyzerInputs(ctx context.Context, request analyzerRequest, inputs scoreInputs, err error) (scoreInputs, error) {
	inputs, err = recoverAnalyzerState(ctx, request, inputs, err)
	if err != nil {
		return inputs, err
	}
	inputs, err = withSourceDepthEstimates(ctx, request, inputs)
	if err != nil {
		return inputs, err
	}
	if state := analysisProgress(ctx); state != nil {
		for _, unit := range request.Units {
			for _, path := range unit.Paths {
				if err := emitAnalysisFile(ctx, inputs, path); err != nil {
					return inputs, err
				}
			}
		}
	}
	return inputs, nil
}

func recoverAnalyzerState(ctx context.Context, request analyzerRequest, inputs scoreInputs, err error) (scoreInputs, error) {
	if fatal := fatalAnalyzerError(ctx, err); fatal != nil {
		return scoreInputs{}, fmt.Errorf("%s analyzer failed: %w", requestLanguage(request), fatal)
	}
	if err == nil {
		return inputs, nil
	}
	if isTerminalFailure(err) {
		inputs = retainTerminalFailureDetail(inputs, err)
		if hasFailedCoverage(inputs) {
			return fillMissingFailureCoverage(request, inputs, err), nil
		}
	}
	// A tool failure cannot establish that its provisional measurements are complete.
	failed := failedAnalyzerInputs(request, err)
	failed.diagnostics = append(inputs.diagnostics, failed.diagnostics...)
	return failed, nil
}
func hasFailedCoverage(inputs scoreInputs) bool {
	for _, components := range inputs.coverage {
		for _, state := range components {
			if state == "failed" {
				return true
			}
		}
	}
	return false
}
func failedAnalyzerInputs(request analyzerRequest, err error) scoreInputs {
	inputs := newScoreInputs()
	for _, unit := range request.Units {
		for _, path := range unit.Paths {
			inputs.languages[path] = unit.Language
			inputs.coverage[path] = failedComponents(request.Components)
			inputs.diagnostics = append(inputs.diagnostics, analyzerFailureDiagnostic(unit, path, err.Error()))
		}
	}
	return inputs
}
func failedComponents(components []requestedComponent) map[string]string {
	states := make(map[string]string, len(components))
	for _, component := range components {
		states[component.ID] = "failed"
	}
	return states
}
func fillMissingFailureCoverage(request analyzerRequest, inputs scoreInputs, err error) scoreInputs {
	for _, unit := range request.Units {
		for _, path := range unit.Paths {
			inputs.languages[path] = unit.Language
			if fillMissingPathCoverage(&inputs, path, request.Components) {
				inputs.diagnostics = append(inputs.diagnostics, analyzerFailureDiagnostic(unit, path, "omitted required coverage: "+err.Error()))
			}
		}
	}
	return inputs
}
func fillMissingPathCoverage(inputs *scoreInputs, path string, components []requestedComponent) bool {
	missing := false
	if inputs.coverage[path] == nil {
		inputs.coverage[path] = map[string]string{}
	}
	for _, component := range components {
		if inputs.coverage[path][component.ID] != "" {
			continue
		}
		inputs.coverage[path][component.ID] = "failed"
		delete(inputs.observations[path], component.ID)
		missing = true
	}
	return missing
}
func analyzerFailureDiagnostic(unit protocolUnit, path, message string) map[string]any {
	return map[string]any{
		"type": "diagnostic", "severity": "error", "code": "native.analyzer_failed",
		"unit_id": unit.ID, "path": path,
		"message": unit.Language + " analyzer failed: " + message,
	}
}

func requestLanguage(request analyzerRequest) string {
	if len(request.Units) == 0 {
		return "requested"
	}
	return request.Units[0].Language
}

func retainTerminalFailureDetail(inputs scoreInputs, err error) scoreInputs {
	for _, diagnostic := range inputs.diagnostics {
		if diagnostic["code"] == "native.terminal_failure" {
			diagnostic["message"] = err.Error()
			return inputs
		}
	}
	inputs.diagnostics = append(inputs.diagnostics, terminalDiagnostic(protocolRecord{}, err).Raw)
	return inputs
}
