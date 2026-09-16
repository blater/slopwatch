package codexcli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func (client *appServerReader) read(reader io.Reader) {
	maximum := diagnosticCaptureLimit
	if client.responses.maximum > 0 {
		maximum = int(client.responses.maximum)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(64<<10, maximum)), maximum)
	var consumed int64
	for scanner.Scan() {
		line := scanner.Bytes()
		consumed += int64(len(line)) + 1
		if client.responses.maximum > 0 && consumed > client.responses.maximum {
			client.responses.mu.Lock()
			client.responses.readErr = fmt.Errorf("Codex App Server output exceeded the configured %d-byte budget", client.responses.maximum)
			client.responses.mu.Unlock()
			break
		}
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			client.responses.mu.Lock()
			client.responses.readErr = fmt.Errorf("decode Codex App Server message: %w", err)
			client.responses.mu.Unlock()
			break
		}
		if client.readResponse(message) {
			continue
		}
		handled, fatal := client.rejectRequest(message)
		if handled {
			if fatal {
				break
			}
			continue
		}
		if client.handler != nil && message.Method != "" {
			client.handler(message)
		}
	}
	client.finishRead(scanner.Err())
}

func (client *appServerReader) readResponse(message rpcMessage) bool {
	if len(message.ID) == 0 || message.Method != "" {
		return false
	}
	var id int64
	if err := json.Unmarshal(message.ID, &id); err != nil {
		return true
	}
	client.responses.mu.Lock()
	pending := client.responses.pending[id]
	delete(client.responses.pending, id)
	client.responses.mu.Unlock()
	if pending == nil {
		return true
	}
	if message.Error != nil {
		pending <- rpcResponse{err: message.Error}
	} else {
		pending <- rpcResponse{result: message.Result}
	}
	return true
}

func (client *appServerReader) rejectRequest(message rpcMessage) (bool, bool) {
	if len(message.ID) == 0 || message.Method == "" {
		return false, false
	}
	if client.writer.tryWrite(map[string]any{"id": json.RawMessage(message.ID), "error": map[string]any{"code": -32601, "message": "unsupported server request"}}) {
		return true, false
	}
	client.responses.mu.Lock()
	client.responses.readErr = errors.New("Codex App Server produced more unsupported requests than could be rejected")
	client.responses.mu.Unlock()
	return true, true
}

func (client *appServerReader) finishRead(scanErr error) {
	client.responses.mu.Lock()
	if scanErr != nil {
		if client.responses.maximum > 0 {
			client.responses.readErr = fmt.Errorf("read Codex App Server output within the configured %d-byte budget: %w", client.responses.maximum, scanErr)
		} else {
			client.responses.readErr = fmt.Errorf("read Codex App Server output: %w", scanErr)
		}
	}
	err := client.responses.readErr
	if err == nil {
		err = io.EOF
	}
	for id, pending := range client.responses.pending {
		delete(client.responses.pending, id)
		pending <- rpcResponse{err: err}
	}
	client.responses.mu.Unlock()
	close(client.responses.done)
}
