package codexcli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type fakeRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params map[string]any  `json:"params"`
}

type fakeResponder struct {
	id json.RawMessage
}

func (responder fakeResponder) result(value any) {
	encoded, _ := json.Marshal(map[string]any{"id": json.RawMessage(responder.id), "result": value})
	fmt.Println(string(encoded))
}

func (responder fakeResponder) failure(message string) {
	encoded, _ := json.Marshal(map[string]any{"id": json.RawMessage(responder.id), "error": map[string]any{"code": -32602, "message": message}})
	fmt.Println(string(encoded))
}

func fakeNotify(method string, params any) {
	encoded, _ := json.Marshal(map[string]any{"method": method, "params": params})
	fmt.Println(string(encoded))
}

func handleFakeRequest(mode, capture string, request fakeRequest) bool {
	responder := fakeResponder{id: request.ID}
	switch request.Method {
	case "initialize":
		return handleFakeInitialize(mode, responder)
	case "initialized":
	case "account/read":
		responder.result(map[string]any{"account": map[string]any{"type": "chatgpt", "planType": "plus", "email": "test@example.com"}, "requiresOpenaiAuth": true})
	case "model/list":
		responder.result(fakeModelCatalog())
	case "thread/start":
		if stringField(request.Params, "sandbox") != "workspace-write" {
			responder.failure("invalid thread sandbox")
			return true
		}
		responder.result(map[string]any{"thread": map[string]any{"id": "thread-1"}})
	case "turn/start":
		return handleFakeTurnStart(mode, capture, request, responder)
	case "turn/interrupt":
		responder.result(map[string]any{})
		fakeNotify("turn/completed", fakeTurn("interrupted"))
	case "thread/read":
		handleFakeThreadRead(mode, responder)
	}
	return true
}

func handleFakeInitialize(mode string, responder fakeResponder) bool {
	if mode == "blockinit" {
		return true
	}
	responder.result(map[string]any{"userAgent": "codex-cli/0.149.1 (test)"})
	if mode == "readerjoin" {
		fakeNotify("warning", map[string]any{"message": "reader is active"})
		return false
	}
	return true
}

func fakeModelCatalog() map[string]any {
	return map[string]any{"data": []any{map[string]any{
		"model": "gpt-5.6-sol", "displayName": "GPT-5.6-Sol", "isDefault": true,
		"defaultReasoningEffort": "high", "supportedReasoningEfforts": []any{
			map[string]any{"reasoningEffort": "low", "description": "Low"}, map[string]any{"reasoningEffort": "high", "description": "High"},
		},
	}}, "nextCursor": nil}
}

func handleFakeTurnStart(mode, capture string, request fakeRequest, responder fakeResponder) bool {
	sandbox, _ := request.Params["sandboxPolicy"].(map[string]any)
	if stringField(sandbox, "type") != "workspaceWrite" {
		responder.failure("invalid turn sandbox")
		return true
	}
	responder.result(map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress", "items": []any{}}})
	fakeNotify("turn/started", fakeIdentity())
	switch mode {
	case "serverrequest":
		fakeServerRequest()
		time.Sleep(50 * time.Millisecond)
	case "descendant":
		startFakeDescendant(capture)
	case "stubborn":
		startTermIgnoringDescendant(capture)
	case "foreign":
		fakeForeignTurn()
	case "oversize":
		fakeOversizeMessage()
	case "flood":
		fakeWarnings(4, 0)
	case "actoroverflow":
		fakeActorOverflow()
	case "cumulative":
		fakeWarnings(160, 8192)
	case "oversizeframe":
		fakeNotify("warning", map[string]any{"message": strings.Repeat("x", diagnosticCaptureLimit+1)})
		return false
	}
	if standardFakeMode(mode) {
		fakeStandardTurn(mode)
	}
	return mode != "completeexit"
}

func fakeServerRequest() {
	encoded, _ := json.Marshal(map[string]any{"id": 99, "method": "item/permissions/requestApproval", "params": map[string]any{"threadId": "thread-1", "turnId": "turn-1"}})
	fmt.Println(string(encoded))
}

func standardFakeMode(mode string) bool {
	for _, value := range []string{"complete", "completeexit", "cumulative", "foreign", "flood", "descendant", "stubborn", "serverrequest", "trailing", "lostcompletion", "lostactive", "lostforeign", "lostmissing", "readblockedcomplete", "readblockedcancel"} {
		if mode == value {
			return true
		}
	}
	return false
}

func fakeIdentity() map[string]any { return map[string]any{"threadId": "thread-1", "turnId": "turn-1"} }
func fakeTurn(status string) map[string]any {
	return map[string]any{"threadId": "thread-1", "turn": map[string]any{"id": "turn-1", "status": status, "items": []any{}}}
}

func fakeForeignTurn() {
	fakeNotify("item/completed", map[string]any{"threadId": "foreign-thread", "turnId": "foreign-turn", "item": map[string]any{"id": "foreign", "type": "agentMessage", "text": "foreign result"}})
	fakeNotify("turn/completed", map[string]any{"threadId": "foreign-thread", "turn": map[string]any{"id": "foreign-turn", "status": "completed", "items": []any{}}})
}

func fakeOversizeMessage() {
	fakeNotify("item/completed", map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{"id": "large", "type": "agentMessage", "text": strings.Repeat("x", 8192)}})
}

func fakeWarnings(count, size int) {
	message := "warning"
	if size > 0 {
		message = strings.Repeat("x", size)
	}
	for index := 0; index < count; index++ {
		fakeNotify("warning", map[string]any{"message": fmt.Sprintf("%s %d", message, index)})
	}
}

func fakeActorOverflow() {
	for _, actor := range []string{"actor-1", "actor-2"} {
		fakeNotify("item/completed", mergeMap(fakeIdentity(), map[string]any{"item": map[string]any{"id": actor, "type": "collabAgentToolCall", "receiverThreadIds": []any{actor}}}))
	}
}

func fakeStandardTurn(mode string) {
	identity := fakeIdentity()
	fakeNotify("item/started", mergeMap(identity, map[string]any{"item": map[string]any{"id": "cmd-1", "type": "commandExecution", "command": "go test ./..."}}))
	fakeNotify("item/completed", mergeMap(identity, map[string]any{"item": map[string]any{"id": "change-1", "type": "fileChange", "changes": []any{map[string]any{"path": "main.go"}}}}))
	fakeNotify("item/completed", mergeMap(identity, map[string]any{"item": map[string]any{"id": "message-1", "type": "agentMessage", "phase": "final_answer", "text": "Fixed the target."}}))
	fakeNotify("thread/tokenUsage/updated", mergeMap(identity, map[string]any{"tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 20, "cachedInputTokens": 5, "outputTokens": 8, "reasoningOutputTokens": 3}}}))
	if !containsString([]string{"lostcompletion", "lostactive", "lostforeign", "lostmissing", "readblockedcomplete", "readblockedcancel"}, mode) {
		fakeNotify("turn/completed", fakeTurn("completed"))
	}
	if mode == "trailing" {
		fakeNotify("warning", map[string]any{"message": "late provider warning"})
	}
}

func handleFakeThreadRead(mode string, responder fakeResponder) {
	if mode == "readblockedcomplete" {
		fakeNotify("turn/completed", fakeTurn("completed"))
		return
	}
	if mode == "readblockedcancel" {
		return
	}
	threadID, status := "thread-1", "idle"
	if mode == "lostactive" {
		status = "active"
	}
	if mode == "lostforeign" {
		threadID = "foreign-thread"
	}
	if mode == "lostmissing" {
		threadID = ""
	}
	thread := map[string]any{"status": map[string]any{"type": status}, "turns": []any{}}
	if threadID != "" {
		thread["id"] = threadID
	}
	responder.result(map[string]any{"thread": thread})
}
