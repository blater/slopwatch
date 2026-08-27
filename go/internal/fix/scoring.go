package fix

type MetricID string

const MetricScore MetricID = "score"

type MetricValue struct {
	ID       MetricID
	Label    string
	Value    float64
	Complete bool
}

type MetricGoal struct {
	Metric  MetricID
	Maximum float64
}

type ScoringGoal struct {
	MaximumScore      float64
	Focus             []MetricGoal
	AllowedRegression map[MetricID]float64
}

type MetricEvidence struct {
	Metric  MetricID
	Summary string
	Values  []float64
	Paths   []RepoPath
}

type TargetSnapshot struct {
	Path        RepoPath
	ContentHash string
	Language    string
	Score       float64
	Metrics     map[MetricID]MetricValue
	Evidence    []MetricEvidence
	Complete    bool
}

type ScoringContract struct {
	CatalogID       string
	ProfileSetHash  string
	Targets         []TargetSnapshot
	Goal            ScoringGoal
	RequireComplete bool
}
