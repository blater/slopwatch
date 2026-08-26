package codexcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// appServerClient owns one Codex App Server child and its bidirectional JSONL
// protocol. It is deliberately private to the adapter: orchestration sees only
// the provider-neutral Strategy contract.
type appServerClient struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser

	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan rpcResponse
	handler  func(rpcMessage)
	stderr   boundedText
	outbound chan outboundMessage
	stop     chan struct{}
	stopOne  sync.Once

	done       chan struct{}
	writerDone chan struct{}
	waited     chan error
	readErr    error
	closeOne   sync.Once
	maximum    int64
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcResponse struct {
	result json.RawMessage
	err    error
}

type outboundMessage struct {
	data   []byte
	result chan error
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (value *rpcError) Error() string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("Codex App Server error %d: %s", value.Code, sanitizeProviderText(value.Message))
}

func startAppServer(executable, directory string, environment []string, maximum int64, handler func(rpcMessage)) (*appServerClient, error) {
	if maximum <= 0 || maximum > int64(^uint(0)>>1) {
		return nil, errors.New("Codex App Server output budget must be a positive supported byte count")
	}
	command := exec.Command(executable, "app-server")
	command.Dir = directory
	command.Env = environment
	configureAppServerProcess(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Codex App Server stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open Codex App Server stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open Codex App Server stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start Codex App Server: %w", err)
	}
	client := &appServerClient{
		command: command, stdin: stdin, stdout: stdout, pending: make(map[int64]chan rpcResponse),
		handler: handler, done: make(chan struct{}), waited: make(chan error, 1),
		stderr: boundedText{maximum: diagnosticCaptureLimit}, maximum: maximum,
		outbound: make(chan outboundMessage, 64), stop: make(chan struct{}), writerDone: make(chan struct{}),
	}
	go func() {
		_, _ = io.Copy(&client.stderr, stderr)
	}()
	go client.read(stdout)
	go client.writeLoop()
	go func() { client.waited <- command.Wait() }()
	return client, nil
}

func (client *appServerClient) read(reader io.Reader) {
	maximum := int(client.maximum)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(64<<10, maximum)), maximum)
	var consumed int64
	for scanner.Scan() {
		line := scanner.Bytes()
		consumed += int64(len(line)) + 1
		if consumed > client.maximum {
			client.mu.Lock()
			client.readErr = fmt.Errorf("Codex App Server output exceeded the configured %d-byte budget", client.maximum)
			client.mu.Unlock()
			break
		}
		var message rpcMessage
		if err := json.Unmarshal(line, &message); err != nil {
			client.mu.Lock()
			client.readErr = fmt.Errorf("decode Codex App Server message: %w", err)
			client.mu.Unlock()
			break
		}
		if len(message.ID) > 0 && message.Method == "" {
			var id int64
			if err := json.Unmarshal(message.ID, &id); err == nil {
				client.mu.Lock()
				pending := client.pending[id]
				delete(client.pending, id)
				client.mu.Unlock()
				if pending != nil {
					if message.Error != nil {
						pending <- rpcResponse{err: message.Error}
					} else {
						pending <- rpcResponse{result: message.Result}
					}
				}
			}
			continue
		}
		if len(message.ID) > 0 && message.Method != "" {
			// Fix jobs use approvalPolicy=never and do not expose interactive
			// provider requests. Fail unexpected requests explicitly so Codex
			// cannot wait forever for a UI that this workflow does not provide.
			if !client.tryWrite(map[string]any{"id": json.RawMessage(message.ID), "error": map[string]any{"code": -32601, "message": "unsupported server request"}}) {
				client.mu.Lock()
				client.readErr = errors.New("Codex App Server produced more unsupported requests than could be rejected")
				client.mu.Unlock()
				break
			}
			continue
		}
		if client.handler != nil && message.Method != "" {
			client.handler(message)
		}
	}
	if err := scanner.Err(); err != nil {
		client.mu.Lock()
		client.readErr = fmt.Errorf("read Codex App Server output within the configured %d-byte budget: %w", client.maximum, err)
		client.mu.Unlock()
	}
	client.mu.Lock()
	err := client.readErr
	if err == nil {
		err = io.EOF
	}
	for id, pending := range client.pending {
		delete(client.pending, id)
		pending <- rpcResponse{err: err}
	}
	client.mu.Unlock()
	close(client.done)
}

func (client *appServerClient) Request(ctx context.Context, method string, params any, destination any) error {
	client.mu.Lock()
	client.nextID++
	id := client.nextID
	pending := make(chan rpcResponse, 1)
	client.pending[id] = pending
	client.mu.Unlock()
	if err := client.write(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		client.removePending(id)
		return err
	}
	select {
	case response := <-pending:
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
	case <-ctx.Done():
		client.removePending(id)
		return ctx.Err()
	case <-client.done:
		client.removePending(id)
		return client.protocolExitError()
	}
}

func (client *appServerClient) Notify(ctx context.Context, method string, params any) error {
	return client.write(ctx, map[string]any{"method": method, "params": params})
}

func encodeOutbound(message any) ([]byte, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func (client *appServerClient) write(ctx context.Context, message any) error {
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

func (client *appServerClient) tryWrite(message any) bool {
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

func (client *appServerClient) writeLoop() {
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

func (client *appServerClient) removePending(id int64) {
	client.mu.Lock()
	delete(client.pending, id)
	client.mu.Unlock()
}

func (client *appServerClient) protocolExitError() error {
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

func (client *appServerClient) Close(grace time.Duration) error {
	var closeErr error
	client.closeOne.Do(func() {
		client.stopOne.Do(func() { close(client.stop) })
		_ = client.stdin.Close()
		select {
		case <-client.waited:
		case <-time.After(grace):
			_ = terminateAppServerProcess(client.command)
			select {
			case <-client.waited:
			case <-time.After(grace):
				_ = killAppServerProcess(client.command)
				// A detached descendant may still hold the inherited stdout file
				// description. Closing our read end prevents it from keeping Wait
				// and the package-owned reader alive after the owned server dies.
				_ = client.stdout.Close()
				select {
				case <-client.waited:
				case <-time.After(grace):
					closeErr = errors.Join(closeErr, errors.New("Codex App Server process did not stop after forced cancellation"))
				}
			}
		}
		// App Server can have ordinary background descendants in its process
		// group. Stop that owned group on every close, including successful
		// completion, before verification can begin.
		if appServerProcessGroupAlive(client.command) {
			_ = terminateAppServerProcess(client.command)
			deadline := time.NewTimer(grace)
			ticker := time.NewTicker(10 * time.Millisecond)
			for appServerProcessGroupAlive(client.command) {
				select {
				case <-ticker.C:
				case <-deadline.C:
					_ = killAppServerProcess(client.command)
					goto waitAfterKill
				}
			}
		waitAfterKill:
			ticker.Stop()
			if !deadline.Stop() {
				select {
				case <-deadline.C:
				default:
				}
			}
			if appServerProcessGroupAlive(client.command) {
				killDeadline := time.NewTimer(grace)
				killTicker := time.NewTicker(10 * time.Millisecond)
				for appServerProcessGroupAlive(client.command) {
					select {
					case <-killTicker.C:
					case <-killDeadline.C:
						closeErr = errors.Join(closeErr, errors.New("Codex App Server process group did not stop after cancellation"))
						goto killWaitFinished
					}
				}
			killWaitFinished:
				killTicker.Stop()
				if !killDeadline.Stop() {
					select {
					case <-killDeadline.C:
					default:
					}
				}
			}
		}
		_ = client.stdout.Close()
		select {
		case <-client.done:
		case <-time.After(grace):
			closeErr = errors.Join(closeErr, errors.New("Codex App Server output reader did not stop"))
		}
		select {
		case <-client.writerDone:
		case <-time.After(grace):
			closeErr = errors.Join(closeErr, errors.New("Codex App Server input writer did not stop"))
		}
	})
	return closeErr
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
