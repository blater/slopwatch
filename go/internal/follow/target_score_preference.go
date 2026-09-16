package follow

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
)

// targetScorePreferenceState serializes target-score preference writes while
// retaining the newest value selected by the user. The owning Model supplies
// the current preferences and handles dialog/notice presentation.
type targetScorePreferenceState struct {
	desired float64
	saving  bool
}

// targetScorePreferenceOutcome carries the resolved preferences that the
// Model should project into its fix dialog and whether a newer selection must
// be written after this save settles.
type targetScorePreferenceOutcome struct {
	preferences appconfig.Resolved
	command     tea.Cmd
}

func (state *targetScorePreferenceState) request(score float64, preferences appconfig.Resolved, store ConfigStore, workspace fix.WorkspaceIdentity) tea.Cmd {
	state.desired = score
	if state.saving || store == nil {
		return nil
	}
	state.saving = true
	return targetScorePreferenceSaveCommand(store, workspace, score, preferences)
}

func (state *targetScorePreferenceState) complete(message fixTargetPreferenceSavedMsg, current appconfig.Resolved, store ConfigStore, workspace fix.WorkspaceIdentity) targetScorePreferenceOutcome {
	state.saving = false
	preferences := current
	if message.err == nil {
		preferences = message.saved.Resolved
	}
	var command tea.Cmd
	if state.desired != message.score {
		command = state.request(state.desired, preferences, store, workspace)
	}
	return targetScorePreferenceOutcome{
		preferences: preferences, command: command,
	}
}

func targetScorePreferenceSaveCommand(store ConfigStore, workspace fix.WorkspaceIdentity, score float64, preferences appconfig.Resolved) tea.Cmd {
	revision := preferences.Revision
	defaults := cloneConfigFix(preferences.Fix)
	defaults.TargetScore = score
	return func() tea.Msg {
		saved, err := store.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Fix: &defaults}, revision)
		if errors.Is(err, appconfig.ErrRevisionConflict) {
			resolved, resolveErr := store.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
			if resolveErr != nil {
				err = resolveErr
			} else {
				defaults = cloneConfigFix(resolved.Fix)
				defaults.TargetScore = score
				saved, err = store.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Fix: &defaults}, resolved.Revision)
			}
		}
		return fixTargetPreferenceSavedMsg{score: score, saved: saved, err: err}
	}
}
