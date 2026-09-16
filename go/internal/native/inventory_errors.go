package native

import "errors"

// ErrNoSources reports an empty source inventory. Callers such as follow mode
// can clear a previous view for this case while continuing to surface other
// discovery, parser, and analyzer failures as hard errors.
var ErrNoSources = errors.New("no supported source files found")

func requireDiscoveredSources(discovered map[string][]string) error {
	for _, paths := range discovered {
		if len(paths) > 0 {
			return nil
		}
	}
	return ErrNoSources
}
