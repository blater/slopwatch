package codexcli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

type normalized struct {
	summary          string
	usage            agent.Usage
	sessionReference string
}

func normalizeEvents(data []byte, request agent.Request, sink agent.EventSink) (normalized, error) {
	return normalizeEventReader(bytes.NewReader(data), request, sink)
}

func normalizeEventReader(reader io.Reader, request agent.Request, sink agent.EventSink) (normalized, error) {
	var result normalized
	if sink == nil {
		sink = agent.EventSinkFunc(func(agent.Event) error { return nil })
	}
	maximum := request.Limits.MaxEvents
	maximumLine := request.Limits.MaxOutputBytes
	maximumActors := request.Limits.MaxActors
	if maximumLine <= 0 {
		return result, fmt.Errorf("Codex output limit must be configured")
	}
	if maximumLine > int64(^uint(0)>>1) {
		maximumLine = int64(^uint(0) >> 1)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(64<<10, int(maximumLine))), int(maximumLine))
	sequence := uint64(0)
	seen := 0
	actors := map[string]struct{}{}
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		seen++
		if maximum > 0 && seen > maximum {
			return result, fmt.Errorf("Codex emitted more than %d events", maximum)
		}
		var raw map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			return result, fmt.Errorf("invalid Codex JSONL event %d: %w", seen, err)
		}
		events := translateEvent(raw, request, &result)
		for _, event := range events {
			if event.ActorID != "" {
				actors[event.ActorID] = struct{}{}
				if maximumActors > 0 && len(actors) > maximumActors {
					return result, fmt.Errorf("Codex reported more than %d actors", maximumActors)
				}
			}
			sequence++
			event.Sequence = sequence
			if event.At.IsZero() {
				event.At = time.Now()
			}
			if err := sink.Emit(event); err != nil {
				return result, fmt.Errorf("emit Codex event: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read Codex JSONL: %w", err)
	}
	return result, nil
}

func translateEvent(raw map[string]any, request agent.Request, result *normalized) []agent.Event {
	typeName, _ := raw["type"].(string)
	base := agent.Event{JobID: request.JobID, AttemptID: request.AttemptID}
	if value, ok := raw["thread_id"].(string); ok {
		result.sessionReference = value
	}
	switch typeName {
	case "thread.started", "turn.started":
		base.Kind = agent.EventStarted
		base.Summary = humanize(typeName)
		return []agent.Event{base}
	case "error":
		base.Kind = agent.EventWarning
		base.Summary = stringField(raw, "message")
		return []agent.Event{base}
	case "turn.completed":
		if usage, ok := mapField(raw, "usage"); ok {
			result.usage = parseUsage(usage)
			base.Kind = agent.EventUsage
			base.Usage = &result.usage
			base.Summary = "Token usage updated"
			return []agent.Event{base}
		}
	case "item.started", "item.updated", "item.completed":
		item, ok := mapField(raw, "item")
		if !ok {
			return nil
		}
		return translateItem(typeName, item, base, result)
	}
	return nil
}

func translateItem(eventType string, item map[string]any, base agent.Event, result *normalized) []agent.Event {
	itemType := stringField(item, "type")
	base.CommandID = stringField(item, "id")
	base.ActorID = stringField(item, "agent_id")
	base.ParentActorID = stringField(item, "parent_agent_id")
	switch itemType {
	case "command_execution":
		base.Summary = stringField(item, "command")
		if eventType == "item.started" {
			base.Kind = agent.EventCommandStarted
		} else if eventType == "item.completed" {
			base.Kind = agent.EventCommandFinished
		} else {
			base.Kind = agent.EventActivity
		}
		return []agent.Event{base}
	case "agent_message", "reasoning":
		base.Kind = agent.EventRuntimeMessage
		base.Summary = stringField(item, "text")
		if itemType == "agent_message" && eventType == "item.completed" && base.Summary != "" {
			result.summary = base.Summary
		}
		return []agent.Event{base}
	case "file_change":
		changes, _ := item["changes"].([]any)
		events := make([]agent.Event, 0, len(changes))
		for _, changeValue := range changes {
			change, ok := changeValue.(map[string]any)
			if !ok {
				continue
			}
			path, err := fix.ParseRepoPath(strings.TrimPrefix(stringField(change, "path"), "./"))
			if err != nil {
				continue
			}
			event := base
			event.Kind = agent.EventFileChanged
			event.Path = path
			event.Summary = "Changed " + path.String()
			events = append(events, event)
		}
		return events
	}
	return nil
}

func parseUsage(value map[string]any) agent.Usage {
	return agent.Usage{
		InputTokens: integerField(value, "input_tokens"), CachedTokens: integerField(value, "cached_input_tokens"),
		OutputTokens: integerField(value, "output_tokens"), ReasoningTokens: integerField(value, "reasoning_output_tokens"),
		Cumulative: true,
	}
}

func mapField(value map[string]any, key string) (map[string]any, bool) {
	result, ok := value[key].(map[string]any)
	return result, ok
}

func stringField(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

func integerField(value map[string]any, key string) int64 {
	switch result := value[key].(type) {
	case float64:
		return int64(result)
	case json.Number:
		integer, _ := result.Int64()
		return integer
	default:
		return 0
	}
}

func humanize(value string) string {
	return strings.Title(strings.ReplaceAll(value, ".", " "))
}
