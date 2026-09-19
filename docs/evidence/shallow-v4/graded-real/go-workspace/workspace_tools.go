package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/blater/slopwatch/internal/fix"
)

func encodeToolValue(value any, maximum int64) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fault("tool_failed", "tool result could not be encoded")
	}
	if int64(len(payload)) > maximum {
		return "", fault("result_too_large", "tool result exceeds the configured output limit")
	}
	return string(payload), nil
}

func encodeToolError(err error) string {
	code := "tool_failed"
	message := "tool operation failed"
	var typed *toolFault
	if errors.As(err, &typed) {
		code = typed.code
		message = typed.message
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "canceled"
		message = "tool operation was canceled"
	}
	payload, marshalErr := json.Marshal(map[string]any{
		"ok":    false,
		"error": map[string]string{"code": code, "message": message},
	})
	if marshalErr != nil {
		return `{"ok":false,"error":{"code":"tool_failed","message":"tool operation failed"}}`
	}
	return string(payload)
}

func (tools *candidateTools) execute(ctx context.Context, call functionCall) (string, *fix.RepoPath, error) {
	var value any
	var changed *fix.RepoPath
	switch call.Name {
	case "read_target_manifest":
		if err := decodeNoArguments(call.Arguments); err != nil {
			return "", nil, err
		}
		var err error
		value, err = tools.readTargetManifest(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
	case "list_files":
		arguments, err := decodeListArguments(call.Arguments)
		if err != nil {
			return "", nil, err
		}
		value, err = tools.list(ctx, arguments.Path, arguments.Recursive)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
	case "read_file":
		arguments, err := decodePathArguments(call.Arguments)
		if err != nil {
			return "", nil, err
		}
		value, err = tools.read(ctx, arguments.Path)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
	case "write_file":
		arguments, err := decodeWriteArguments(call.Arguments)
		if err != nil {
			return "", nil, err
		}
		value, err = tools.write(ctx, arguments.Path, arguments.Content)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
		parsed, _ := fix.ParseRepoPath(arguments.Path)
		changed = &parsed
	case "delete_file":
		arguments, err := decodePathArguments(call.Arguments)
		if err != nil {
			return "", nil, err
		}
		value, err = tools.delete(ctx, arguments.Path)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", nil, err
			}
			return encodeToolError(err), nil, nil
		}
		parsed, _ := fix.ParseRepoPath(arguments.Path)
		changed = &parsed
	default:
		return "", nil, fmt.Errorf("unknown function tool %q", call.Name)
	}
	encoded, err := encodeToolValue(value, tools.config.maxToolOutputBytes)
	return encoded, changed, err
}
