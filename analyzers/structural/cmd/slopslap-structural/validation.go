package main

import "fmt"

func validate(input request) error {
	if input.Type != "request" || input.Version != protocolVersion || input.Invocation == "" || input.Workspace == "" {
		return fmt.Errorf("unsupported structural analyzer request")
	}
	if len(input.Units) == 0 || len(input.Components) == 0 {
		return fmt.Errorf("request requires units and components")
	}
	seen := make(map[string]struct{})
	for _, item := range input.Components {
		expected, ok := componentVersions[item.ID]
		if !ok || item.Version != expected {
			return fmt.Errorf("unsupported component %s@%s", item.ID, item.Version)
		}
		key := item.ID + "@" + item.Version
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate component %s", key)
		}
		seen[key] = struct{}{}
	}
	unitIDs := make(map[string]struct{}, len(input.Units))
	pathOwners := make(map[string]string)
	for _, item := range input.Units {
		if item.ID == "" || item.Language == "" || len(item.Paths) == 0 {
			return fmt.Errorf("invalid structural analysis unit")
		}
		if _, duplicate := unitIDs[item.ID]; duplicate {
			return fmt.Errorf("duplicate analysis unit %s", item.ID)
		}
		unitIDs[item.ID] = struct{}{}
		for _, path := range item.Paths {
			if owner, duplicate := pathOwners[path]; duplicate {
				return fmt.Errorf("source path %s belongs to both %s and %s", path, owner, item.ID)
			}
			pathOwners[path] = item.ID
		}
	}
	return nil
}
