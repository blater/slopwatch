package main

import "slopslap.dev/structural/internal/metrics"

func requestUsesDepthV4(input request) bool {
	for _, component := range input.Components {
		if component.ID == "module_shallowness" && component.Version == metrics.DepthV4Definition {
			return true
		}
	}
	return false
}
func requestStrategies(input request) *metrics.Registry {
	if requestUsesDepthV4(input) {
		return metrics.DepthV4Registry()
	}
	return strategyRegistry
}
func requestAdapterOptions(input request) map[string]any {
	options := make(map[string]any, len(input.Options)+1)
	for key, value := range input.Options {
		options[key] = value
	}
	if requestUsesDepthV4(input) {
		options["depth_profile"] = "responsibility-v4"
	} else {
		delete(options, "depth_profile")
	}
	return options
}
