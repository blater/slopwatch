package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/blater/slopwatch/internal/naming"
)

type requestedComponent struct {
	ID      string `json:"component_id"`
	Version string `json:"definition_version"`
}

type protocolUnit struct {
	ID       string         `json:"unit_id"`
	Language string         `json:"language"`
	Paths    []string       `json:"source_paths"`
	Metadata map[string]any `json:"metadata"`
}

type analyzerRequest struct {
	Type       string               `json:"type"`
	Version    int                  `json:"protocol_version"`
	Invocation string               `json:"invocation_id"`
	Workspace  string               `json:"workspace"`
	Units      []protocolUnit       `json:"units"`
	Components []requestedComponent `json:"components"`
	Options    map[string]any       `json:"options"`
	Limits     map[string]int       `json:"limits"`
}

type protocolRecord struct {
	Type                  string          `json:"type"`
	Version               int             `json:"protocol_version"`
	Invocation            string          `json:"invocation_id"`
	UnitID                string          `json:"unit_id"`
	Component             string          `json:"component_id"`
	Definition            string          `json:"definition_version"`
	Path                  *string         `json:"path"`
	Language              string          `json:"language"`
	Scope                 string          `json:"scope"`
	Value                 any             `json:"value"`
	Subject               protocolSubject `json:"subject"`
	Attributes            map[string]any  `json:"attributes"`
	Provenance            map[string]any  `json:"provenance"`
	State                 string          `json:"state"`
	Reason                string          `json:"reason"`
	Severity              string          `json:"severity"`
	Code                  string          `json:"code"`
	Message               string          `json:"message"`
	Status                string          `json:"status"`
	ParserModes           []string        `json:"parser_modes"`
	Kernels               []string        `json:"kernels"`
	DiscoveredSourceCount int             `json:"discovered_source_count"`
	ParsedSourceCount     int             `json:"parsed_source_count"`
	Raw                   map[string]any  `json:"-"`
}

type protocolSubject struct {
	Name      string           `json:"name"`
	Symbol    string           `json:"symbol"`
	Routine   string           `json:"routine"`
	Line      int              `json:"line"`
	Column    int              `json:"column"`
	EndLine   int              `json:"end_line"`
	EndColumn int              `json:"end_column"`
	Start     protocolPosition `json:"start"`
	End       protocolPosition `json:"end"`
}

type protocolPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

func invocationID() (string, error) {
	return naming.New("invocation")
}

func runAnalyzer(ctx context.Context, executable string, request analyzerRequest) (scoreInputs, error) {
	return runAnalyzerDecoded(ctx, executable, request, decodeScoreInputs)
}

func runAnalyzerUnits(ctx context.Context, executable string, request analyzerRequest) (map[string]scoreInputs, error) {
	return runAnalyzerDecoded(ctx, executable, request, decodeUnitScoreInputs)
}

func runAnalyzerDecoded[T any](ctx context.Context, executable string, request analyzerRequest, decode func(io.Reader, analyzerRequest) (T, error)) (T, error) {
	var zero T
	payload, err := json.Marshal(request)
	if err != nil {
		return zero, err
	}
	payload = append(payload, '\n')
	workdir, err := os.MkdirTemp("", "slopslap-analyzer-")
	if err != nil {
		return zero, err
	}
	defer os.RemoveAll(workdir)
	command := exec.CommandContext(ctx, executable)
	command.Dir = workdir
	command.Stdin = bytes.NewReader(payload)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return zero, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	path := os.Getenv("PATH")
	command.Env = []string{"PATH=" + path, "LANG=C.UTF-8", "SLOPSLAP_WORK_DIR=" + workdir, "SLOPSLAP_CACHE_DIR=" + filepath.Join(workdir, "cache"), "GOROOT=" + runtime.GOROOT()}
	if err := os.Mkdir(filepath.Join(workdir, "cache"), 0o700); err != nil {
		return zero, err
	}
	if err := command.Start(); err != nil {
		return zero, err
	}
	result, decodeErr := decode(stdout, request)
	if decodeErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return zero, fmt.Errorf("analyzer canceled: %w", ctxErr)
	}
	if waitErr != nil && strings.TrimSpace(stderr.String()) != "" {
		return zero, fmt.Errorf("analyzer failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if decodeErr != nil {
		return zero, decodeErr
	}
	if waitErr != nil {
		return zero, fmt.Errorf("analyzer failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return result, nil
}

func decodeProtocol(reader io.Reader, request analyzerRequest, consume func(protocolRecord) error) error {
	stream := json.NewDecoder(reader)
	stream.UseNumber()
	terminal := false
	for {
		var record protocolRecord
		if err := stream.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("invalid analyzer protocol: %w", err)
		}
		if terminal {
			return fmt.Errorf("protocol record after terminal status")
		}
		switch record.Type {
		case "diagnostic":
			record.Raw = map[string]any{
				"type": record.Type, "protocol_version": record.Version,
				"invocation_id": record.Invocation, "unit_id": record.UnitID,
				"path": record.Path, "severity": record.Severity,
				"code": record.Code, "message": record.Message,
			}
		case "execution_plan":
			record.Raw = map[string]any{
				"type": record.Type, "protocol_version": record.Version,
				"invocation_id": record.Invocation, "unit_id": record.UnitID,
				"parser_modes": record.ParserModes, "kernels": record.Kernels,
				"discovered_source_count": record.DiscoveredSourceCount,
				"parsed_source_count":     record.ParsedSourceCount,
			}
		}
		if record.Version != 1 || record.Invocation != request.Invocation {
			return fmt.Errorf("mismatched analyzer protocol record")
		}
		if record.Type == "terminal" {
			terminal = true
			if record.Status != "success" {
				return fmt.Errorf("analyzer terminal status %s: %s", record.Status, record.Message)
			}
		}
		if err := consume(record); err != nil {
			return err
		}
	}
	if !terminal {
		return fmt.Errorf("analyzer protocol ended without terminal status")
	}
	return nil
}

func decodeRecords(reader io.Reader, request analyzerRequest) ([]protocolRecord, error) {
	records := make([]protocolRecord, 0)
	err := decodeProtocol(reader, request, func(record protocolRecord) error {
		records = append(records, record)
		return nil
	})
	return records, err
}

func decodeScoreInputs(reader io.Reader, request analyzerRequest) (scoreInputs, error) {
	inputs := newScoreInputs()
	err := decodeProtocol(reader, request, inputs.add)
	return inputs, err
}

func decodeUnitScoreInputs(reader io.Reader, request analyzerRequest) (map[string]scoreInputs, error) {
	units := make(map[string]scoreInputs, len(request.Units))
	for _, unit := range request.Units {
		units[unit.ID] = newScoreInputs()
	}
	err := decodeProtocol(reader, request, func(record protocolRecord) error {
		if record.UnitID == "" {
			return nil
		}
		inputs, exists := units[record.UnitID]
		if !exists {
			return fmt.Errorf("protocol record references unknown unit %q", record.UnitID)
		}
		if err := inputs.add(record); err != nil {
			return err
		}
		units[record.UnitID] = inputs
		return nil
	})
	return units, err
}
