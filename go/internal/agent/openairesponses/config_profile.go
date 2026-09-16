package openairesponses

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/blater/slopwatch/internal/agent"
)

func (config resolvedConfig) withProfile(profile agent.Profile) (resolvedConfig, error) {
	result := config
	known := make(map[string]struct{}, len(profileLimitFields))
	for _, definition := range profileLimitFields {
		known[definition.key] = struct{}{}
		text, exists := profile.Options[definition.key]
		if !exists || text == "" {
			continue
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil || value < 0 {
			return resolvedConfig{}, fmt.Errorf("%s must be a non-negative integer", definition.label)
		}
		if (definition.key == "max_turns" || definition.key == "max_tool_calls" || definition.key == "max_output_tokens" || definition.key == "max_list_entries" || definition.key == "max_summary_bytes") &&
			uint64(value) > uint64(^uint(0)>>1) {
			return resolvedConfig{}, fmt.Errorf("%s is too large for this platform", definition.label)
		}
		switch definition.key {
		case "max_turns":
			result.maxTurns = int(value)
		case "max_tool_calls":
			result.maxToolCalls = int(value)
		case "max_output_tokens":
			result.maxOutputTokens = int(value)
		case "max_context_tokens":
			result.maxContextTokens = value
		case "max_response_bytes":
			result.maxResponseBytes = value
		case "max_probe_bytes":
			result.maxProbeBytes = value
		case "max_request_bytes":
			result.maxRequestBytes = value
		case "max_context_bytes":
			result.maxContextBytes = value
		case "max_tool_output_bytes":
			result.maxToolOutputBytes = value
		case "max_read_bytes":
			result.maxReadBytes = value
		case "max_write_bytes":
			result.maxWriteBytes = value
		case "max_list_entries":
			result.maxListEntries = int(value)
		case "max_summary_bytes":
			result.maxSummaryBytes = int(value)
		}
	}
	for key := range profile.Options {
		if _, ok := known[key]; !ok {
			return resolvedConfig{}, fmt.Errorf("unsupported Responses API profile option %q", key)
		}
	}
	if result.maxResponseBytes <= 0 || result.maxProbeBytes <= 0 || result.maxRequestBytes <= 0 || result.maxContextBytes <= 0 || result.maxToolOutputBytes <= 0 ||
		result.maxReadBytes <= 0 || result.maxWriteBytes <= 0 || result.maxListEntries <= 0 || result.maxSummaryBytes <= 0 {
		return resolvedConfig{}, errors.New("byte, file-list, and summary limits must be positive")
	}
	if result.maxReadBytes > result.maxToolOutputBytes {
		return resolvedConfig{}, errors.New("file read bytes must not exceed tool result bytes")
	}
	if result.maxResponseBytes > result.maxContextBytes || result.maxToolOutputBytes > result.maxContextBytes {
		return resolvedConfig{}, errors.New("response and tool result bytes must not exceed local context bytes")
	}
	return result, nil
}
