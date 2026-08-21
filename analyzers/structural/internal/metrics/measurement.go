package metrics

import "slopslap.dev/structural/internal/facts"

type Measurement struct {
	Component  string
	Definition string
	Scope      string
	Value      any
	Subject    string
	Location   facts.Location
	Attributes map[string]any
}
