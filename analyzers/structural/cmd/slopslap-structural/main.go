// Command slopslap-structural is the protocol-v1 structural analyzer host.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"sort"

	"slopslap.dev/structural/internal/adapters"
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/goadapter"
	"slopslap.dev/structural/internal/javaadapter"
	"slopslap.dev/structural/internal/metrics"
	"slopslap.dev/structural/internal/rustadapter"
)

const (
	protocolVersion = 1
	analyzerName    = "slopslap-structural"
	analyzerVersion = "0.1.0"
)

var strategyRegistry = metrics.DefaultRegistry()
var componentVersions = strategyRegistry.Definitions()

type component struct {
	ID      string `json:"component_id"`
	Version string `json:"definition_version"`
}

type unit struct {
	ID       string         `json:"unit_id"`
	Language string         `json:"language"`
	Paths    []string       `json:"source_paths"`
	Metadata map[string]any `json:"metadata"`
}

type request struct {
	Type       string         `json:"type"`
	Version    int            `json:"protocol_version"`
	Invocation string         `json:"invocation_id"`
	Workspace  string         `json:"workspace"`
	Units      []unit         `json:"units"`
	Components []component    `json:"components"`
	Options    map[string]any `json:"options"`
	Limits     map[string]int `json:"limits"`
}

type emitter struct {
	invocation string
	encoder    *json.Encoder
}

func (e emitter) emit(recordType string, values map[string]any) {
	record := map[string]any{
		"type": recordType, "protocol_version": protocolVersion,
		"invocation_id": e.invocation,
	}
	for key, value := range values {
		record[key] = value
	}
	if err := e.encoder.Encode(record); err != nil {
		os.Exit(2)
	}
}

func encodedInteger(value *big.Int) any {
	limit := big.NewInt(9_007_199_254_740_991)
	abs := new(big.Int).Abs(new(big.Int).Set(value))
	if abs.Cmp(limit) <= 0 {
		return value.Int64()
	}
	return map[string]string{"$integer": value.String()}
}

func encodedValue(value any) any {
	if integer, ok := value.(*big.Int); ok {
		return encodedInteger(integer)
	}
	return value
}

func subject(measurement metrics.Measurement) map[string]any {
	result := map[string]any{
		"name": measurement.Subject, "symbol": measurement.Subject,
		"line": measurement.Location.Line, "column": measurement.Location.Column,
		"end_line": measurement.Location.EndLine, "end_column": measurement.Location.EndColumn,
	}
	if measurement.Scope == "function" {
		result["routine"] = measurement.Subject
	} else if routine, ok := measurement.Attributes["routine_symbol"].(string); ok {
		result["routine"] = routine
	}
	return result
}

func failUnit(out emitter, item unit, components []component, message string) {
	out.emit("diagnostic", map[string]any{
		"severity": "error", "code": "STRUCTURAL_ANALYZER", "message": message,
		"unit_id": item.ID, "path": nil,
	})
	for _, path := range item.Paths {
		for _, requested := range components {
			out.emit("coverage", map[string]any{
				"unit_id": item.ID, "component_id": requested.ID,
				"definition_version": requested.Version, "path": path,
				"state": "failed", "reason": message,
			})
		}
	}
}

func emitProgramCoverage(out emitter, item unit, components []component, program *facts.Program) {
	failedPaths := make(map[string]bool, len(program.Failures))
	for _, failure := range program.Failures {
		firstFailure := !failedPaths[failure.Path]
		failedPaths[failure.Path] = true
		out.emit("diagnostic", map[string]any{
			"severity": "error", "code": failure.Code, "message": failure.Diagnostic,
			"unit_id": item.ID, "path": failure.Path,
		})
		if !firstFailure {
			continue
		}
		for _, requested := range components {
			out.emit("coverage", map[string]any{
				"unit_id": item.ID, "component_id": requested.ID,
				"definition_version": requested.Version, "path": failure.Path,
				"state": "failed", "reason": failure.Diagnostic,
			})
		}
	}
	warnings := make(map[string]map[string]bool)
	for _, path := range item.Paths {
		if failedPaths[path] {
			continue
		}
		for _, requested := range components {
			state, reason := "complete", ""
			if available, detail := program.Availability(path, requested.ID); !available {
				state, reason = "unavailable", detail
				if reason != "" {
					if warnings[path] == nil {
						warnings[path] = make(map[string]bool)
					}
					if !warnings[path][reason] {
						out.emit("diagnostic", map[string]any{
							"severity": "warning", "code": "COVERAGE_UNAVAILABLE",
							"message": reason, "unit_id": item.ID, "path": path,
						})
						warnings[path][reason] = true
					}
				}
			}
			out.emit("coverage", map[string]any{
				"unit_id": item.ID, "component_id": requested.ID,
				"definition_version": requested.Version, "path": path,
				"state": state, "reason": reason,
			})
		}
	}
}

func run(input request, writer io.Writer) int {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	out := emitter{input.Invocation, encoder}
	if err := validate(input); err != nil {
		out.emit("terminal", map[string]any{
			"status": "failure", "message": err.Error(),
			"analyzed_unit_ids": []string{}, "failed_unit_ids": []string{},
			"skipped_unit_ids": []string{},
		})
		return 0
	}
	requested := make(map[string]bool, len(input.Components))
	kernels := make([]string, 0, len(input.Components))
	for _, item := range input.Components {
		requested[item.ID] = true
		kernels = append(kernels, item.ID)
	}
	sort.Strings(kernels)
	strategies := requestStrategies(input)
	adapterOptions := requestAdapterOptions(input)
	analyzed := make([]string, 0, len(input.Units))
	failed := make([]string, 0)
	adapterRegistry, registryErr := adapters.NewRegistry(
		goadapter.Adapter{}, javaadapter.Adapter{}, rustadapter.Adapter{},
	)
	if registryErr != nil {
		out.emit("terminal", map[string]any{
			"status": "failure", "message": registryErr.Error(),
			"analyzed_unit_ids": []string{}, "failed_unit_ids": []string{},
			"skipped_unit_ids": []string{},
		})
		return 0
	}
	for _, item := range input.Units {
		languageAdapter, adapterErr := adapterRegistry.Adapter(item.Language)
		if adapterErr != nil {
			failUnit(out, item, input.Components, adapterErr.Error())
			failed = append(failed, item.ID)
			continue
		}
		program, err := languageAdapter.Analyze(input.Workspace, item.Paths, adapterOptions)
		if err != nil {
			failUnit(out, item, input.Components, err.Error())
			failed = append(failed, item.ID)
			continue
		}
		measurements, strategyErr := strategies.Analyze(program, requested)
		if strategyErr != nil {
			failUnit(out, item, input.Components, strategyErr.Error())
			failed = append(failed, item.ID)
			continue
		}
		for _, measurement := range measurements {
			out.emit("measurement", map[string]any{
				"unit_id": item.ID, "component_id": measurement.Component,
				"definition_version": measurement.Definition,
				"path":               measurement.Location.Path, "language": item.Language,
				"scope": measurement.Scope, "value": encodedValue(measurement.Value),
				"subject": subject(measurement), "attributes": measurement.Attributes,
				"provenance": map[string]any{
					"analyzer": analyzerName, "analyzer_version": analyzerVersion,
					"rule": measurement.Component + "/" + measurement.Definition,
				},
			})
		}
		emitProgramCoverage(out, item, input.Components, program)
		out.emit("execution_plan", map[string]any{
			"unit_id": item.ID, "parser_modes": languageAdapter.ParserModes(),
			"kernels": kernels, "discovered_source_count": len(item.Paths),
			"parsed_source_count": len(program.Files),
		})
		analyzed = append(analyzed, item.ID)
	}
	status := "success"
	message := ""
	if len(failed) > 0 {
		status = "failure"
		message = "one or more structural analysis units failed"
	}
	out.emit("terminal", map[string]any{
		"status": status, "message": message, "analyzed_unit_ids": analyzed,
		"failed_unit_ids": failed, "skipped_unit_ids": []string{},
	})
	return 0
}

func decodeRequest(reader io.Reader) (request, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var input request
	if err := decoder.Decode(&input); err != nil {
		return request{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return request{}, err
		}
		return request{}, errors.New("unexpected trailing analyzer request data")
	}
	return input, nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--evaluate-depth-v4" {
		if err := runDepthEvaluator(os.Stdin, os.Stdout); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	input, err := decodeRequest(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	os.Exit(run(input, os.Stdout))
}
