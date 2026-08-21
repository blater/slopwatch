package native

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	Type       string          `json:"type"`
	Version    int             `json:"protocol_version"`
	Invocation string          `json:"invocation_id"`
	UnitID     string          `json:"unit_id"`
	Component  string          `json:"component_id"`
	Definition string          `json:"definition_version"`
	Path       *string         `json:"path"`
	Language   string          `json:"language"`
	Scope      string          `json:"scope"`
	Value      any             `json:"value"`
	Subject    protocolSubject `json:"subject"`
	Attributes map[string]any  `json:"attributes"`
	State      string          `json:"state"`
	Reason     string          `json:"reason"`
	Severity   string          `json:"severity"`
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	Status     string          `json:"status"`
	Raw        map[string]any  `json:"-"`
}

type protocolSubject struct {
	Name      string `json:"name"`
	Symbol    string `json:"symbol"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
}

func invocationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	text := hex.EncodeToString(value[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", text[:8], text[8:12], text[12:16], text[16:20], text[20:]), nil
}

func runAnalyzer(ctx context.Context, executable string, request analyzerRequest, timeoutSeconds float64) ([]protocolRecord, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')
	workdir, err := os.MkdirTemp("", "slopslap-analyzer-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workdir)
	command := exec.CommandContext(ctx, executable)
	command.Dir = workdir
	command.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	path := os.Getenv("PATH")
	command.Env = []string{"PATH=" + path, "LANG=C.UTF-8", "SLOPSLAP_WORK_DIR=" + workdir, "SLOPSLAP_CACHE_DIR=" + filepath.Join(workdir, "cache"), "GOROOT=" + runtime.GOROOT()}
	if err := os.Mkdir(filepath.Join(workdir, "cache"), 0o700); err != nil {
		return nil, err
	}
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("analyzer timed out after %.6g seconds", timeoutSeconds)
		}
		return nil, fmt.Errorf("analyzer failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() > 32*1024*1024 {
		return nil, fmt.Errorf("analyzer protocol output exceeds 32 MiB")
	}
	return decodeRecords(&stdout, request)
}

func decodeRecords(reader io.Reader, request analyzerRequest) ([]protocolRecord, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var records []protocolRecord
	terminal := false
	for scanner.Scan() {
		if terminal {
			return nil, fmt.Errorf("protocol record after terminal status")
		}
		var raw map[string]any
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.UseNumber()
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("invalid analyzer protocol: %w", err)
		}
		encoded, _ := json.Marshal(raw)
		decoder = json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		var record protocolRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, err
		}
		record.Raw = raw
		if record.Version != 1 || record.Invocation != request.Invocation {
			return nil, fmt.Errorf("mismatched analyzer protocol record")
		}
		if record.Type == "terminal" {
			terminal = true
			if record.Status != "success" {
				return nil, fmt.Errorf("analyzer terminal status %s: %s", record.Status, record.Message)
			}
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !terminal {
		return nil, fmt.Errorf("analyzer protocol ended without terminal status")
	}
	return records, nil
}
