package native

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func decodeProtocol(reader io.Reader, request analyzerRequest, consume func(protocolRecord) error) error {
	diagnostics := []string{}
	err := decodeProtocolStream(reader, request, func(record protocolRecord) error {
		if record.Type == "diagnostic" && record.Message != "" {
			diagnostics = append(diagnostics, record.Message)
		}
		return consume(record)
	})
	if err != nil && !isTerminalFailure(err) {
		if len(diagnostics) > 0 {
			err = fmt.Errorf("%w; analyzer diagnostics: %s", err, strings.Join(diagnostics, "; "))
		}
		return &analyzerProtocolError{err}
	}
	return err
}

func decodeProtocolStream(reader io.Reader, request analyzerRequest, consume func(protocolRecord) error) error {
	stream := json.NewDecoder(reader)
	stream.UseNumber()
	terminal := false
	var terminalErr error
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
		record.attachMetadata()
		if record.Version != 1 || record.Invocation != request.Invocation {
			return fmt.Errorf("mismatched analyzer protocol record")
		}
		if record.Type == "terminal" {
			terminal = true
			var err error
			terminalErr, err = consumeTerminal(record, consume)
			if err != nil {
				return err
			}
		}
		if err := consume(record); err != nil {
			return err
		}
	}
	if !terminal {
		return errMissingTerminal
	}
	return terminalErr
}

func consumeTerminal(record protocolRecord, consume func(protocolRecord) error) (error, error) {
	switch record.Status {
	case "success":
		return nil, nil
	case "failure":
		failure := &analyzerTerminalFailure{status: record.Status, message: record.Message}
		return failure, consume(terminalDiagnostic(record, failure))
	default:
		return nil, fmt.Errorf("invalid analyzer terminal status %q", record.Status)
	}
}
