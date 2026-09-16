package preferencesadapter

import (
	"reflect"

	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/preferences"
)

func markProfileEntryOrigins(origins map[string]appconfig.Origin, before, after []preferences.AgentProfile, origin appconfig.Origin) {
	prior := make(map[string]preferences.AgentProfile, len(before))
	for _, profile := range before {
		prior[profile.ID] = profile
	}
	for _, profile := range after {
		old, exists := prior[profile.ID]
		prefix := "agents." + profile.ID + "."
		if !exists || !reflect.DeepEqual(old, profile) {
			origins["agents."+profile.ID] = origin
		}
		markProfileFields(origins, prefix, old, profile, exists, origin)
		markProfileOptions(origins, prefix, old.Options, profile.Options, exists, origin)
	}
}

func markProfileFields(origins map[string]appconfig.Origin, prefix string, before, after preferences.AgentProfile, exists bool, origin appconfig.Origin) {
	fields := []struct {
		key     string
		changed bool
	}{{"label", !exists || before.Label != after.Label}, {"runtime", !exists || before.Runtime != after.Runtime}, {"executable", !exists || before.Executable != after.Executable}, {"runtime_profile", !exists || before.RuntimeProfile != after.RuntimeProfile}, {"authentication_ref", !exists || before.AuthenticationRef != after.AuthenticationRef}, {"options", !exists || !reflect.DeepEqual(before.Options, after.Options)}}
	for _, field := range fields {
		if field.changed {
			origins[prefix+field.key] = origin
		}
	}
}

func markProfileOptions(origins map[string]appconfig.Origin, prefix string, before, after map[string]string, exists bool, origin appconfig.Origin) {
	for key, value := range after {
		if !exists || before[key] != value {
			origins[prefix+"options."+key] = origin
		}
	}
}
