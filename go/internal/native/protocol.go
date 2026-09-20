package native

type requestedComponent struct {
	ID      string `json:"component_id"`
	Version string `json:"definition_version"`
}

type protocolUnit struct {
	ID       string         `json:"unit_id"`
	Language string         `json:"language"`
	Paths    []string       `json:"source_paths"`
	Metadata map[string]any `json:"metadata"`
}

type analyzerRequest struct {
	Type       string               `json:"type"`
	Version    int                  `json:"protocol_version"`
	Invocation string               `json:"invocation_id"`
	Workspace  string               `json:"workspace"`
	Units      []protocolUnit       `json:"units"`
	Components []requestedComponent `json:"components"`
	Options    map[string]any       `json:"options"`
	Limits     map[string]int       `json:"limits"`
}

type protocolRecord struct {
	Type                  string          `json:"type"`
	Version               int             `json:"protocol_version"`
	Invocation            string          `json:"invocation_id"`
	UnitID                string          `json:"unit_id"`
	Component             string          `json:"component_id"`
	Definition            string          `json:"definition_version"`
	Path                  *string         `json:"path"`
	Language              string          `json:"language"`
	Scope                 string          `json:"scope"`
	Value                 any             `json:"value"`
	Subject               protocolSubject `json:"subject"`
	Attributes            map[string]any  `json:"attributes"`
	Provenance            map[string]any  `json:"provenance"`
	State                 string          `json:"state"`
	Reason                string          `json:"reason"`
	Severity              string          `json:"severity"`
	Code                  string          `json:"code"`
	Message               string          `json:"message"`
	Status                string          `json:"status"`
	ParserModes           []string        `json:"parser_modes"`
	Kernels               []string        `json:"kernels"`
	DiscoveredSourceCount int             `json:"discovered_source_count"`
	ParsedSourceCount     int             `json:"parsed_source_count"`
	Stage                 string          `json:"stage"`
	Completed             int             `json:"completed"`
	Total                 int             `json:"total"`
	Files                 int             `json:"files"`
	Raw                   map[string]any  `json:"-"`
}

type protocolSubject struct {
	Name      string           `json:"name"`
	Symbol    string           `json:"symbol"`
	Routine   string           `json:"routine"`
	Line      int              `json:"line"`
	Column    int              `json:"column"`
	EndLine   int              `json:"end_line"`
	EndColumn int              `json:"end_column"`
	Start     protocolPosition `json:"start"`
	End       protocolPosition `json:"end"`
}

type protocolPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

func (record *protocolRecord) attachMetadata() {
	switch record.Type {
	case "diagnostic":
		var diagnosticPath any
		if record.Path != nil {
			diagnosticPath = *record.Path
		}
		record.Raw = map[string]any{
			"type": record.Type, "protocol_version": record.Version,
			"invocation_id": record.Invocation, "unit_id": record.UnitID,
			"path": diagnosticPath, "severity": record.Severity,
			"code": record.Code, "message": record.Message,
		}
		if len(record.Attributes) > 0 {
			record.Raw["attributes"] = record.Attributes
		}
	case "execution_plan":
		record.Raw = map[string]any{
			"type": record.Type, "protocol_version": record.Version,
			"invocation_id": record.Invocation, "unit_id": record.UnitID,
			"parser_modes": record.ParserModes, "kernels": record.Kernels,
			"discovered_source_count": record.DiscoveredSourceCount,
			"parsed_source_count":     record.ParsedSourceCount,
		}
	}

}
