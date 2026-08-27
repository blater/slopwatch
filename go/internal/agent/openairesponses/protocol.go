package openairesponses

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type apiRequest struct {
	Model             string            `json:"model"`
	Input             []json.RawMessage `json:"input"`
	Tools             []toolDefinition  `json:"tools"`
	ToolChoice        string            `json:"tool_choice"`
	ParallelToolCalls bool              `json:"parallel_tool_calls"`
	Reasoning         reasoningRequest  `json:"reasoning"`
	MaxOutputTokens   int               `json:"max_output_tokens,omitempty"`
	Store             bool              `json:"store"`
	Truncation        string            `json:"truncation"`
}

type reasoningRequest struct {
	Effort string `json:"effort"`
}

type toolDefinition struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

func functionTools(hasTargetManifest bool) []toolDefinition {
	pathProperty := map[string]any{
		"type":        "string",
		"description": "Slash-separated path relative to the candidate repository root.",
	}
	tools := []toolDefinition{
		{
			Type: "function", Name: "list_files",
			Description: "List bounded candidate file metadata. Symlinks, special files, and .git are blocked.",
			Parameters: objectSchema(map[string]any{
				"path":      pathProperty,
				"recursive": map[string]any{"type": "boolean", "description": "Whether to recursively list directories."},
			}, "path", "recursive"), Strict: true,
		},
		{
			Type: "function", Name: "read_file",
			Description: "Read one bounded regular file from the candidate.",
			Parameters:  objectSchema(map[string]any{"path": pathProperty}, "path"), Strict: true,
		},
		{
			Type: "function", Name: "write_file",
			Description: "Atomically replace or create an allowed regular candidate file with complete content.",
			Parameters: objectSchema(map[string]any{
				"path":    pathProperty,
				"content": map[string]any{"type": "string", "description": "Complete replacement file content."},
			}, "path", "content"), Strict: true,
		},
		{
			Type: "function", Name: "delete_file",
			Description: "Delete one allowed regular candidate file.",
			Parameters:  objectSchema(map[string]any{"path": pathProperty}, "path"), Strict: true,
		},
	}
	if hasTargetManifest {
		tools = append(tools, toolDefinition{
			Type: "function", Name: "read_target_manifest",
			Description: "Read the complete newline-delimited selected target list supplied by Slopmochi.",
			Parameters:  objectSchema(map[string]any{}), Strict: true,
		})
	}
	return tools
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

type apiResponse struct {
	ID                   string            `json:"id"`
	Object               string            `json:"object"`
	CreatedAt            json.RawMessage   `json:"created_at"`
	Status               string            `json:"status"`
	Background           json.RawMessage   `json:"background"`
	Billing              json.RawMessage   `json:"billing"`
	CompletedAt          json.RawMessage   `json:"completed_at"`
	Conversation         json.RawMessage   `json:"conversation"`
	Error                *apiError         `json:"error"`
	IncompleteDetails    json.RawMessage   `json:"incomplete_details"`
	Instructions         json.RawMessage   `json:"instructions"`
	MaxOutputTokens      json.RawMessage   `json:"max_output_tokens"`
	MaxToolCalls         json.RawMessage   `json:"max_tool_calls"`
	Model                string            `json:"model"`
	Moderation           json.RawMessage   `json:"moderation"`
	Output               []json.RawMessage `json:"output"`
	OutputText           json.RawMessage   `json:"output_text"`
	ParallelToolCalls    json.RawMessage   `json:"parallel_tool_calls"`
	PreviousResponseID   json.RawMessage   `json:"previous_response_id"`
	Prompt               json.RawMessage   `json:"prompt"`
	PromptCacheKey       json.RawMessage   `json:"prompt_cache_key"`
	PromptCacheOptions   json.RawMessage   `json:"prompt_cache_options"`
	PromptCacheRetention json.RawMessage   `json:"prompt_cache_retention"`
	Reasoning            json.RawMessage   `json:"reasoning"`
	SafetyIdentifier     json.RawMessage   `json:"safety_identifier"`
	ServiceTier          json.RawMessage   `json:"service_tier"`
	Store                json.RawMessage   `json:"store"`
	Temperature          json.RawMessage   `json:"temperature"`
	Text                 json.RawMessage   `json:"text"`
	ToolChoice           json.RawMessage   `json:"tool_choice"`
	Tools                json.RawMessage   `json:"tools"`
	TopLogprobs          json.RawMessage   `json:"top_logprobs"`
	TopP                 json.RawMessage   `json:"top_p"`
	Truncation           json.RawMessage   `json:"truncation"`
	Usage                *apiUsage         `json:"usage"`
	User                 json.RawMessage   `json:"user"`
	Metadata             json.RawMessage   `json:"metadata"`
}

type apiError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Param   json.RawMessage `json:"param"`
	Type    string          `json:"type"`
}

type apiUsage struct {
	InputTokens        int64               `json:"input_tokens"`
	InputTokenDetails  *inputTokenDetails  `json:"input_tokens_details"`
	OutputTokens       int64               `json:"output_tokens"`
	OutputTokenDetails *outputTokenDetails `json:"output_tokens_details"`
	TotalTokens        int64               `json:"total_tokens"`
}

type inputTokenDetails struct {
	CachedTokens     int64 `json:"cached_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

type outputTokenDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

type outputHeader struct {
	Type string `json:"type"`
}

type outputMessage struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Status  string          `json:"status"`
	Role    string          `json:"role"`
	Phase   json.RawMessage `json:"phase"`
	Content []outputContent `json:"content"`
}

type outputContent struct {
	Type        string            `json:"type"`
	Text        string            `json:"text"`
	Refusal     string            `json:"refusal"`
	Annotations []json.RawMessage `json:"annotations"`
	Logprobs    []json.RawMessage `json:"logprobs"`
}

type functionCall struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type reasoningItem struct {
	ID               string            `json:"id"`
	Type             string            `json:"type"`
	Status           string            `json:"status"`
	Summary          []json.RawMessage `json:"summary"`
	Content          []json.RawMessage `json:"content"`
	EncryptedContent json.RawMessage   `json:"encrypted_content"`
}

type parsedOutput struct {
	calls   []functionCall
	text    string
	refused bool
}

func decodeAPIResponse(payload []byte) (apiResponse, parsedOutput, error) {
	var response apiResponse
	// The response envelope is intentionally forward-compatible: provider
	// metadata evolves independently of Slopmochi. Security-sensitive output
	// items and tool calls below remain strict and deny unknown shapes.
	if err := json.Unmarshal(payload, &response); err != nil {
		return apiResponse{}, parsedOutput{}, errors.New("Responses API returned malformed JSON")
	}
	if response.Status == "" {
		return apiResponse{}, parsedOutput{}, errors.New("Responses API response omitted status")
	}
	var parsed parsedOutput
	var text strings.Builder
	for _, raw := range response.Output {
		var header outputHeader
		if err := json.Unmarshal(raw, &header); err != nil || header.Type == "" {
			return apiResponse{}, parsedOutput{}, errors.New("Responses API returned a malformed output item")
		}
		switch header.Type {
		case "message":
			var message outputMessage
			if err := decodeStrictBytes(raw, &message); err != nil || message.Role != "assistant" {
				return apiResponse{}, parsedOutput{}, errors.New("Responses API returned a malformed assistant message")
			}
			for _, content := range message.Content {
				switch content.Type {
				case "output_text":
					if content.Text == "" {
						return apiResponse{}, parsedOutput{}, errors.New("Responses API returned empty output text")
					}
					if text.Len() > 0 {
						text.WriteByte('\n')
					}
					text.WriteString(content.Text)
				case "refusal":
					parsed.refused = true
				default:
					return apiResponse{}, parsedOutput{}, fmt.Errorf("Responses API returned unsupported message content %q", content.Type)
				}
			}
		case "function_call":
			var call functionCall
			if err := decodeStrictBytes(raw, &call); err != nil || call.CallID == "" || call.Name == "" || call.Arguments == "" {
				return apiResponse{}, parsedOutput{}, errors.New("Responses API returned a malformed function call")
			}
			parsed.calls = append(parsed.calls, call)
		case "reasoning":
			var reasoning reasoningItem
			if err := decodeStrictBytes(raw, &reasoning); err != nil {
				return apiResponse{}, parsedOutput{}, errors.New("Responses API returned a malformed reasoning item")
			}
		default:
			return apiResponse{}, parsedOutput{}, fmt.Errorf("Responses API returned unsupported output item %q", header.Type)
		}
	}
	parsed.text = text.String()
	return response, parsed, nil
}

type listArguments struct {
	Path      *string `json:"path"`
	Recursive *bool   `json:"recursive"`
}

type pathArguments struct {
	Path *string `json:"path"`
}

type writeArguments struct {
	Path    *string `json:"path"`
	Content *string `json:"content"`
}

func decodeNoArguments(value string) error {
	var arguments struct{}
	if err := decodeStrictString(value, &arguments); err != nil {
		return errors.New("read_target_manifest arguments violate the strict tool schema")
	}
	return nil
}

func decodeListArguments(value string) (struct {
	Path      string
	Recursive bool
}, error) {
	var arguments listArguments
	if err := decodeStrictString(value, &arguments); err != nil || arguments.Path == nil || arguments.Recursive == nil {
		return struct {
			Path      string
			Recursive bool
		}{}, errors.New("list_files arguments violate the strict tool schema")
	}
	return struct {
		Path      string
		Recursive bool
	}{Path: *arguments.Path, Recursive: *arguments.Recursive}, nil
}

func decodePathArguments(value string) (struct{ Path string }, error) {
	var arguments pathArguments
	if err := decodeStrictString(value, &arguments); err != nil || arguments.Path == nil {
		return struct{ Path string }{}, errors.New("file tool arguments violate the strict tool schema")
	}
	return struct{ Path string }{Path: *arguments.Path}, nil
}

func decodeWriteArguments(value string) (struct {
	Path    string
	Content string
}, error) {
	var arguments writeArguments
	if err := decodeStrictString(value, &arguments); err != nil || arguments.Path == nil || arguments.Content == nil {
		return struct {
			Path    string
			Content string
		}{}, errors.New("write_file arguments violate the strict tool schema")
	}
	return struct {
		Path    string
		Content string
	}{Path: *arguments.Path, Content: *arguments.Content}, nil
}

func decodeStrictString(value string, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func decodeStrictBytes(value []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func inputMessage(role, text string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"role":    role,
		"content": []map[string]string{{"type": "input_text", "text": text}},
	})
}

func functionOutput(callID, output string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{
		"type": "function_call_output", "call_id": callID, "output": output,
	})
}

func contextSize(history []json.RawMessage) int64 {
	var result int64
	for _, item := range history {
		result += int64(len(item))
	}
	return result
}
