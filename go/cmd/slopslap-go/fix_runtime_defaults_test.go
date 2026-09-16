package main

import (
	"testing"

	"github.com/blater/slopwatch/internal/agent/codexcli"
	"github.com/blater/slopwatch/internal/agent/openairesponses"
	"github.com/blater/slopwatch/internal/preferences"
)

func TestBuiltInOpenAIDefaultUsesCodexAccountLoginAndKeepsAPIKeyAlternative(t *testing.T) {
	value := agentDefaults(preferences.DefaultDocument())
	if value.Fix.Profile != "codex-default" || len(value.Agents.Profiles) != 2 {
		t.Fatalf("agent defaults = %#v", value)
	}
	if value.Fix.Model != "" {
		t.Fatalf("built-in model must come from the selected adapter, got %q", value.Fix.Model)
	}
	codex, responses := value.Agents.Profiles[0], value.Agents.Profiles[1]
	if codex.ID != "codex-default" || codex.Runtime != string(codexcli.RuntimeKind) || codex.AuthenticationRef != "provider-owned" {
		t.Fatalf("Codex default profile = %#v", codex)
	}
	if responses.ID != "gpt-default" || responses.Runtime != string(openairesponses.RuntimeKind) || responses.AuthenticationRef != "env:OPENAI_API_KEY" {
		t.Fatalf("Responses alternative profile = %#v", responses)
	}
}
