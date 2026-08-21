package goadapter

import (
	"path/filepath"
	"sort"

	"slopslap.dev/structural/internal/facts"
)

type typeRecord struct {
	fact     *facts.Type
	fields   map[string]struct{}
	identity string
}

func packageID(item source) string {
	return filepath.Dir(item.rel) + "|" + item.file.Name.Name
}

func collectTypes(b *analysisContext, sources []source) []*facts.Type {
	records := collectTypeDeclarations(b, sources)
	attachTypeMethods(b, sources, records)
	return orderedTypeFacts(records)
}

func orderedTypeFacts(records map[string]*typeRecord) []*facts.Type {
	output := make([]*facts.Type, 0, len(records))
	for _, record := range records {
		record.fact.ForeignTypes = uniqueStrings(record.fact.ForeignTypes)
		record.fact.ForeignFields = uniqueStrings(record.fact.ForeignFields)
		output = append(output, record.fact)
	}
	sort.Slice(output, func(left, right int) bool {
		a, current := output[left].Location, output[right].Location
		if a.Path != current.Path {
			return a.Path < current.Path
		}
		if a.Line != current.Line {
			return a.Line < current.Line
		}
		return a.Column < current.Column
	})
	return output
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	output := make([]string, 0, len(seen))
	for value := range seen {
		output = append(output, value)
	}
	sort.Strings(output)
	return output
}
