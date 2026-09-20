package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/blater/slopwatch/internal/naming"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func invocationID() (string, error) {
	return naming.New("invocation")
}

func runAnalyzer(ctx context.Context, executable string, request analyzerRequest) (scoreInputs, error) {
	return runAnalyzerDecoded(ctx, executable, request, func(reader io.Reader, request analyzerRequest) (scoreInputs, error) {
		return decodeScoreInputs(reader, request, analysisFilePreview(ctx, request))
	})
}

func runAnalyzerUnits(ctx context.Context, executable string, request analyzerRequest) (map[string]scoreInputs, error) {
	return runAnalyzerDecoded(ctx, executable, request, func(reader io.Reader, request analyzerRequest) (map[string]scoreInputs, error) {
		return decodeUnitScoreInputs(reader, request, analysisFilePreview(ctx, request))
	})
}

func runAnalyzerDecoded[T any](ctx context.Context, executable string, request analyzerRequest, decode func(io.Reader, analyzerRequest) (T, error)) (T, error) {
	var zero T
	ctx = startSourceDepthRun(ctx, request)
	if analysisProgress(ctx) != nil {
		if request.Options == nil {
			request.Options = map[string]any{}
		}
		request.Options["stream_results"] = true
	}
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
	if decodeErr != nil && !isTerminalFailure(decodeErr) {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return zero, fmt.Errorf("analyzer canceled: %w", ctxErr)
	}
	return result, analyzerProcessError(decodeErr, waitErr, strings.TrimSpace(stderr.String()))
}

func analyzerProcessError(decodeErr, waitErr error, detail string) error {
	if decodeErr != nil {
		if waitErr != nil && errors.Is(decodeErr, errMissingTerminal) {
			return fmt.Errorf("analyzer failed: %w: %s", waitErr, detail)
		}
		if detail != "" {
			return fmt.Errorf("%w; analyzer stderr: %s", decodeErr, detail)
		}
		return decodeErr
	}
	if waitErr != nil {
		return fmt.Errorf("analyzer failed: %w: %s", waitErr, detail)
	}
	return nil
}
