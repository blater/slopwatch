package codexcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// appServerClient owns one Codex App Server child and its bidirectional JSONL
// protocol. It is deliberately private to the adapter: orchestration sees only
// the provider-neutral Strategy contract.
type appServerClient struct {
	*appServerProtocol
	*appServerLifecycle
}

type appServerProtocol struct {
	*appServerResponse
	*appServerReader
	*appServerWriter
}

type appServerResponse struct {
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResponse
	done    chan struct{}
	readErr error
	maximum int64
	stderr  boundedText
	writer  *appServerWriter
}

type appServerReader struct {
	responses *appServerResponse
	writer    *appServerWriter
	handler   func(rpcMessage)
}

type appServerWriter struct {
	stdin      io.WriteCloser
	outbound   chan outboundMessage
	stop       chan struct{}
	stopOne    sync.Once
	writerDone chan struct{}
}

type appServerProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	waited  chan error
}

type appServerLifecycle struct {
	process  *appServerProcess
	protocol *appServerProtocol
	closeOne sync.Once
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
	if maximum < 0 || maximum > int64(^uint(0)>>1) {
		return nil, errors.New("Codex App Server output budget is outside the supported byte range")
	}
	command := exec.Command(executable, "app-server")
	command.Dir, command.Env = directory, environment
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
	client := newAppServerClient(command, stdin, stdout, stderr, handler, maximum)
	return client, nil
}

func newAppServerClient(command *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, stderr io.ReadCloser, handler func(rpcMessage), maximum int64) *appServerClient {
	response := &appServerResponse{
		pending: make(map[int64]chan rpcResponse), done: make(chan struct{}),
		stderr: boundedText{maximum: diagnosticCaptureLimit}, maximum: maximum,
	}
	writer := &appServerWriter{stdin: stdin, outbound: make(chan outboundMessage, 64), stop: make(chan struct{}), writerDone: make(chan struct{})}
	protocol := &appServerProtocol{
		appServerResponse: response,
		appServerReader:   &appServerReader{responses: response, writer: writer, handler: handler},
		appServerWriter:   writer,
	}
	response.writer = writer
	process := &appServerProcess{command: command, stdin: stdin, stdout: stdout, waited: make(chan error, 1)}
	lifecycle := &appServerLifecycle{process: process, protocol: protocol}
	client := &appServerClient{appServerProtocol: protocol, appServerLifecycle: lifecycle}
	go func() { _, _ = io.Copy(&response.stderr, stderr) }()
	go func() {
		<-protocol.done
		process.waited <- command.Wait()
	}()
	go protocol.appServerReader.read(stdout)
	go protocol.appServerWriter.writeLoop()
	return client
}
