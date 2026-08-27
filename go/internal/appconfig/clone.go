package appconfig

import (
	"github.com/blater/slopwatch/internal/agent"
)

func cloneResolved(value Resolved) Resolved {
	result := value
	result.Origins = make(map[string]Origin, len(value.Origins))
	for key, origin := range value.Origins {
		result.Origins[key] = origin
	}
	result.Fix = cloneFixDefaults(value.Fix)
	result.Profiles = cloneProfiles(value.Profiles)
	return result
}

func cloneFixDefaults(value FixDefaults) FixDefaults {
	value.Focus = append(value.Focus[:0:0], value.Focus...)
	return value
}

func cloneProfiles(values []agent.Profile) []agent.Profile {
	result := make([]agent.Profile, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Options = make(map[string]string, len(value.Options))
		for key, option := range value.Options {
			result[index].Options[key] = option
		}
	}
	return result
}
