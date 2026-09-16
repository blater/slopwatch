package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func (client *appServerResponse) Request(ctx context.Context, method string, params any, destination any) error {
	client.mu.Lock()
	client.nextID++
	id := client.nextID
	pending := make(chan rpcResponse, 1)
	client.pending[id] = pending
	client.mu.Unlock()
	defer client.removePending(id)
	if err := client.writer.write(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case response := <-pending:
		return client.decodeResponse(response, method, destination)
	case <-ctx.Done():
		return ctx.Err()
	case <-client.done:
		return client.responseAfterExit(pending, method, destination)
	}
}

func (client *appServerResponse) responseAfterExit(pending <-chan rpcResponse, method string, destination any) error {
	select {
	case response := <-pending:
		return client.decodeResponse(response, method, destination)
	default:
		return client.protocolExitError()
	}
}

func (client *appServerResponse) decodeResponse(response rpcResponse, method string, destination any) error {
	if response.err != nil {
		if errors.Is(response.err, io.EOF) {
			return client.protocolExitError()
		}
		return response.err
	}
	if destination == nil || len(response.result) == 0 || string(response.result) == "null" {
		return nil
	}
	if err := json.Unmarshal(response.result, destination); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	return nil
}

func (client *appServerResponse) Notify(ctx context.Context, method string, params any) error {
	return client.writer.write(ctx, map[string]any{"method": method, "params": params})
}

func encodeOutbound(message any) ([]byte, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func (client *appServerResponse) removePending(id int64) {
	client.mu.Lock()
	delete(client.pending, id)
	client.mu.Unlock()
}

func (client *appServerResponse) protocolExitError() error {
	client.mu.Lock()
	err := client.readErr
	client.mu.Unlock()
	if err != nil {
		return err
	}
	if diagnostic := client.stderr.String(); diagnostic != "" {
		return fmt.Errorf("Codex App Server exited before responding: %s; install or update Codex and run Test again", sanitizeProviderText(diagnostic))
	}
	return errors.New("Codex App Server exited before responding; install or update Codex and run Test again")
}
