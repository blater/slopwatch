package sourceignore

import "path/filepath"

// IgnoredKnown evaluates only loaded policy. It neither reads nor caches unknown
// directories, making it safe for bounded notification intake during discovery.
func (m *Matcher) IgnoredKnown(path string, directory bool) bool {
	if m == nil {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.root, path)
	}
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate := filepath.Dir(path)
	if directory {
		candidate = path
	}
	var missing []string
	for {
		if cached, ok := m.directories[candidate]; ok {
			if cached.ignored {
				return true
			}
			for _, dir := range missing {
				if matches(cached.rules, dir, true) {
					return true
				}
			}
			return matches(cached.rules, path, directory)
		}
		missing = append(missing, candidate)
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return false
		}
		candidate = parent
	}
}

// Reload is used only by an explicit in-session startup rescan.
func (m *Matcher) Reload(disabled bool) error {
	next, err := New(m.root, disabled)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.boundary, m.frozen, m.disabled = next.boundary, false, disabled
	m.directories, m.inputs, m.projectRules = next.directories, next.inputs, next.projectRules
	return nil
}
