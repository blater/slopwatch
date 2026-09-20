package report

// ScanProgress describes real completed work, separate from evidence quality.
// It is live UI state and is never serialized into reports or cache artifacts.
type ScanProgress struct {
	Stage     string
	Completed int
	Total     int
	Files     int
}
