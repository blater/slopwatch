package preferencesadapter

import (
	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/validation"
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
	result.Validation = make([]validation.Plan, len(value.Validation))
	for index, plan := range value.Validation {
		result.Validation[index] = clonePlan(plan)
	}
	return result
}

func clonePlan(value validation.Plan) validation.Plan {
	result := value
	result.Checks = make([]validation.Check, len(value.Checks))
	for index, check := range value.Checks {
		result.Checks[index] = check
		result.Checks[index].Arguments = append([]string(nil), check.Arguments...)
	}
	return result
}
