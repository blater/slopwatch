package codexcli

import (
	"encoding/json"
	"errors"

	"github.com/blater/slopwatch/internal/agent"
)

func (run *appServerRun) handle(message rpcMessage) {
	run.mu.Lock()
	terminal := run.terminal
	run.mu.Unlock()
	if terminal {
		return
	}
	var params map[string]any
	if err := json.Unmarshal(message.Params, &params); err != nil {
		run.setError(err)
		return
	}
	if run.bufferUntilTurnBound(message, params) {
		return
	}
	run.handleBoundParams(message, params)
}

func (run *appServerRun) handleBound(message rpcMessage) {
	run.mu.Lock()
	terminal := run.terminal
	run.mu.Unlock()
	if terminal {
		return
	}
	var params map[string]any
	if err := json.Unmarshal(message.Params, &params); err != nil {
		run.setError(err)
		return
	}
	run.handleBoundParams(message, params)
}

func (run *appServerRun) handleBoundParams(message rpcMessage, params map[string]any) {
	switch message.Method {
	case "turn/started":
		if !run.matchesOwned(params, true) {
			return
		}
		run.emit(agent.Event{Kind: agent.EventStarted, Summary: "Codex turn started"})
	case "item/started", "item/completed":
		if !run.matchesOwned(params, false) {
			return
		}
		item, _ := params["item"].(map[string]any)
		if item != nil {
			run.handleItem(message.Method, item)
		}
	case "thread/tokenUsage/updated":
		if !run.matchesOwned(params, false) {
			return
		}
		usage, _ := params["tokenUsage"].(map[string]any)
		total, _ := usage["total"].(map[string]any)
		value := parseAppServerUsage(total)
		run.emitWithUpdate(agent.Event{Kind: agent.EventUsage, Summary: "Token usage updated", Usage: &value}, func() {
			run.usage = value
		})
	case "turn/plan/updated":
		if !run.matchesOwned(params, false) {
			return
		}
		run.emit(agent.Event{Kind: agent.EventActivity, Summary: "Codex updated its plan"})
	case "warning":
		run.emit(agent.Event{Kind: agent.EventWarning, Summary: stringValue(params, "message")})
	case "configWarning":
		run.emit(agent.Event{Kind: agent.EventWarning, Summary: stringValue(params, "summary")})
	case "turn/completed":
		if !run.matchesOwned(params, true) {
			return
		}
		turn, _ := params["turn"].(map[string]any)
		encoded, _ := json.Marshal(turn)
		var completed turnCompletion
		if err := json.Unmarshal(encoded, &completed); err != nil {
			run.setError(err)
			return
		}
		run.complete(completed)
	}
}

func (run *appServerRun) bufferUntilTurnBound(message rpcMessage, params map[string]any) bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.turnReady || run.threadID == "" {
		return false
	}
	if threadID := stringValue(params, "threadId"); threadID != "" && threadID != run.threadID {
		return false
	}
	run.pendingTurn = append(run.pendingTurn, message)
	return true
}

func (run *appServerRun) complete(completed turnCompletion) {
	// Terminalize synchronously with sink emission before waking Execute. The
	// reader may already have the next JSONL notification buffered.
	run.emitMu.Lock()
	defer run.emitMu.Unlock()
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.terminal {
		return
	}
	run.terminal = true
	select {
	case run.completed <- completed:
	default:
	}
}

func (run *appServerRun) matchesOwned(params map[string]any, allowTurnStart bool) bool {
	threadID, turnID := stringValue(params, "threadId"), stringValue(params, "turnId")
	if allowTurnStart && turnID == "" {
		if turn, ok := params["turn"].(map[string]any); ok {
			turnID = stringValue(turn, "id")
		}
	}
	if threadID == "" || turnID == "" {
		run.setError(errors.New("Codex App Server event omitted thread or turn identity"))
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.threadID == "" || threadID != run.threadID {
		return false
	}
	return run.turnID != "" && turnID == run.turnID
}

func (run *appServerRun) handleItem(method string, item map[string]any) {
	kind, id := stringValue(item, "type"), stringValue(item, "id")
	switch kind {
	case "commandExecution":
		eventKind := agent.EventCommandStarted
		if method == "item/completed" {
			eventKind = agent.EventCommandFinished
		}
		run.emit(agent.Event{Kind: eventKind, CommandID: id, Summary: stringValue(item, "command")})
	case "agentMessage":
		text := stringValue(item, "text")
		if method == "item/completed" && text != "" {
			finalAnswer := stringValue(item, "phase") == "final_answer"
			run.emitWithUpdate(agent.Event{Kind: agent.EventRuntimeMessage, CommandID: id, Summary: text}, func() {
				run.summary = text
				if finalAnswer {
					run.finalAnswer = true
				}
			})
			return
		}
		run.emit(agent.Event{Kind: agent.EventRuntimeMessage, CommandID: id, Summary: text})
	case "reasoning":
		if summary := stringSliceValue(item, "summary"); summary != "" {
			run.emit(agent.Event{Kind: agent.EventRuntimeMessage, CommandID: id, Summary: summary})
		}
	case "fileChange":
		changes, _ := item["changes"].([]any)
		for _, raw := range changes {
			change, _ := raw.(map[string]any)
			if path, ok := run.repoPath(stringValue(change, "path")); ok {
				run.emit(agent.Event{Kind: agent.EventFileChanged, CommandID: id, Path: path, Summary: "Changed " + path.String()})
			}
		}
	case "collabAgentToolCall":
		actor := firstStringValue(item, "receiverThreadIds")
		if actor == "" {
			actor = id
		}
		run.emit(agent.Event{Kind: agent.EventActivity, CommandID: id, ActorID: actor, Summary: "Codex agent activity"})
	}
}
