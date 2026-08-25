package appconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

func TestMemorySnapshotsAreIndependentAndRevisionChecked(t *testing.T) {
	t.Parallel()
	memory := NewMemory(Resolved{
		Origins:  map[string]Origin{"fix": OriginBuiltIn},
		Fix:      FixDefaults{TargetScore: 100},
		Profiles: []agent.Profile{{ID: "codex", Options: map[string]string{"key": "value"}}},
	})
	first, err := memory.Resolve(context.Background(), fixWorkspace(), SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	first.Profiles[0].Options["key"] = "mutated"
	second, _ := memory.Resolve(context.Background(), fixWorkspace(), SessionOverrides{})
	if second.Profiles[0].Options["key"] != "value" {
		t.Fatal("resolved snapshot shared profile options")
	}
	updated := second.Fix
	updated.TargetScore = 80
	saved, err := memory.Save(context.Background(), fixWorkspace(), ScopeUser, Patch{Fix: &updated}, second.Revision)
	if err != nil || saved.Resolved.Fix.TargetScore != 80 {
		t.Fatalf("Save() = %#v, %v", saved, err)
	}
	_, err = memory.Save(context.Background(), fixWorkspace(), ScopeUser, Patch{Fix: &updated}, second.Revision)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale Save() error = %v", err)
	}
}

func fixWorkspace() fix.WorkspaceIdentity { return fix.WorkspaceIdentity{} }
