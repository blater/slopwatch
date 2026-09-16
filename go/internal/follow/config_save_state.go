package follow

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
)

type configSaveRequest struct {
	store      ConfigStore
	workspace  fix.WorkspaceIdentity
	fixService FixService
	patch      appconfig.Patch
	generation uint64
	revision   appconfig.Revision
	kind       configSettingsKind
}

func (state *configSettingsState) prepareSave(catalog agent.ProfileCatalog, store ConfigStore, workspace fix.WorkspaceIdentity, fixService FixService) (configSaveRequest, bool) {
	if !state.dirty {
		state.status = "No changes to save"
		return configSaveRequest{}, false
	}
	if err := validateConfigSettingsWithCatalog(state.kind, state.working, catalog); err != nil {
		state.status = "Cannot save: " + err.Error()
		return configSaveRequest{}, false
	}
	if store == nil {
		state.status = "Cannot save: no configuration service"
		return configSaveRequest{}, false
	}
	patch := configSettingsPatch(state.kind, state.working)
	if state.kind == configAgents && state.defaultChanged {
		fixDefaults := cloneConfigFix(state.working.Fix)
		patch.Fix = &fixDefaults
	}
	state.saving = true
	state.status = "Saving…"
	return configSaveRequest{
		store: store, workspace: workspace, fixService: fixService, patch: patch,
		generation: state.generation, revision: state.resolved.Revision, kind: state.kind,
	}, true
}

func (request configSaveRequest) command() tea.Cmd {
	return func() tea.Msg {
		saved, err := request.store.Save(context.Background(), request.workspace, appconfig.ScopeUser, request.patch, request.revision)
		if err == nil && request.kind == configConcurrency && request.fixService != nil {
			err = request.fixService.Reconfigure(context.Background(), fixapp.RuntimeLimitsFromConcurrency(saved.Resolved.Concurrency))
		}
		return configSavedMsg{generation: request.generation, saved: saved, err: err}
	}
}
