package preferencesadapter

import (
	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
)

func cloneResolved(value appconfig.Resolved) appconfig.Resolved {
	result := value
	result.Origins = make(map[string]appconfig.Origin, len(value.Origins))
	for key, origin := range value.Origins {
		result.Origins[key] = origin
	}
	result.Fix.Focus = append([]fix.MetricID(nil), value.Fix.Focus...)
	result.Profiles = make([]agent.Profile, len(value.Profiles))
	for index, profile := range value.Profiles {
		result.Profiles[index] = profile
		result.Profiles[index].Options = cloneStringMap(profile.Options)
	}
	return result
}
