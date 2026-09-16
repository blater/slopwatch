package codexcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

func (client *appServerWriter) write(ctx context.Context, message any) error {
	encoded, err := encodeOutbound(message)
	if err != nil {
		return err
	}
	request := outboundMessage{data: encoded, result: make(chan error, 1)}
	select {
	case client.outbound <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-client.stop:
		return errors.New("Codex App Server is closing")
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-client.stop:
		return errors.New("Codex App Server is closing")
	}
}

func (client *appServerWriter) tryWrite(message any) bool {
	encoded, err := encodeOutbound(message)
	if err != nil {
		return false
	}
	request := outboundMessage{data: encoded, result: make(chan error, 1)}
	select {
	case client.outbound <- request:
		return true
	case <-client.stop:
		return false
	default:
		return false
	}
}

func (client *appServerWriter) writeLoop() {
	defer close(client.writerDone)
	for {
		select {
		case <-client.stop:
			return
		default:
		}
		select {
		case <-client.stop:
			return
		case request := <-client.outbound:
			_, err := client.stdin.Write(request.data)
			if err != nil {
				err = fmt.Errorf("write Codex App Server message: %w", err)
			}
			request.result <- err
		}
	}
}

type boundedText struct {
	mu      sync.Mutex
	maximum int
	data    []byte
}

func (buffer *boundedText) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	remaining := buffer.maximum - len(buffer.data)
	if remaining > 0 {
		if len(data) > remaining {
			buffer.data = append(buffer.data, data[:remaining]...)
		} else {
			buffer.data = append(buffer.data, data...)
		}
	}
	return len(data), nil
}

func (buffer *boundedText) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return strings.TrimSpace(string(buffer.data))
}
