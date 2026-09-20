package native

import (
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/report"
)

// Resolve identities before count aggregation discards individual locations.
// Copies keep the analyzer observations suitable for other policy projections.
func associateScoringRoutines(raw map[string][]observation) map[string][]observation {
	functions := map[string]report.MeasurementEvidence{}
	for _, id := range []string{"cognitive_complexity", "cyclomatic_method_complexity", "npath_complexity"} {
		for _, item := range raw[id] {
			if item.scope == "function" {
				functions[subjectKey(item.subject)] = measurementEvidence(item)
			}
		}
	}
	bySymbol := map[string][]scoringRange{}
	var all []scoringRange
	for key, evidence := range functions {
		entry := scoringRange{key: key, location: evidence.Location}
		bySymbol[evidence.Routine] = append(bySymbol[evidence.Routine], entry)
		all = append(all, entry)
	}
	indexes := map[string]scoringRangeIndex{}
	for symbol, entries := range bySymbol {
		indexes[symbol] = newScoringRangeIndex(entries)
	}
	allFunctions := newScoringRangeIndex(all)
	result := make(map[string][]observation, len(raw))
	for id, items := range raw {
		result[id] = append([]observation(nil), items...)
		for i := range result[id] {
			item := &result[id][i]
			if item.scope == "function" {
				item.scoringRoutine = subjectKey(item.subject)
				location := measurementEvidence(*item).Location
				if location.Path == "" || location.Start.Line <= 0 || !positionBefore(location.Start, location.End) {
					item.scoringRoutine = "unassociated:" + id + ":" + item.scoringRoutine
				}
			} else if id == "deeply_nested_if" {
				evidence := measurementEvidence(*item)
				index := allFunctions
				if evidence.Routine != "" {
					index = indexes[evidence.Routine]
				}
				item.scoringRoutine = index.enclosing(evidence.Location)
			}
		}
	}
	return result
}

func associateTypeOwners(file *report.File) {
	types := file.Components["cyclomatic_class_complexity"]
	methods := file.Components["cyclomatic_method_complexity"]
	var ranges []scoringRange
	byName := map[string][]string{}
	for i, evidence := range types.Evidence {
		if i >= len(types.Subjects) {
			break
		}
		key := types.Subjects[i].Subject
		ranges = append(ranges, scoringRange{key: key, location: evidence.Location})
		byName[evidence.Symbol] = append(byName[evidence.Symbol], key)
	}
	index := newScoringRangeIndex(ranges)
	for i, evidence := range methods.Evidence {
		if i >= len(methods.Subjects) {
			break
		}
		owner := index.enclosing(evidence.Location)
		// Go receivers and Rust impl methods can sit outside the type's
		// declaration. The analyzer emits Receiver.Method; require one match.
		if owner == "" && (file.Language == "go" || file.Language == "rust") {
			if separator := strings.LastIndex(evidence.Routine, "."); separator >= 0 {
				matches := byName[evidence.Routine[:separator]]
				if len(matches) == 1 {
					owner = matches[0]
				}
			}
		}
		methods.Subjects[i].Owner = owner
	}
	if _, exists := file.Components["cyclomatic_method_complexity"]; exists {
		file.Components["cyclomatic_method_complexity"] = methods
	}
}

type scoringRange struct {
	key      string
	location report.SourceRange
	parents  []int
}

// AST ranges are disjoint or nested. Binary lifting gives O(log n) lookup even
// for deeply nested declarations; crossing or duplicate ranges stay unresolved.
type scoringRangeIndex struct {
	entries []scoringRange
	invalid bool
}

func newScoringRangeIndex(entries []scoringRange) scoringRangeIndex {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].location, entries[j].location
		if positionBefore(a.Start, b.Start) || positionBefore(b.Start, a.Start) {
			return positionBefore(a.Start, b.Start)
		}
		return positionBefore(b.End, a.End)
	})
	index := scoringRangeIndex{entries: entries}
	var stack []int
	for i := range entries {
		current := entries[i].location
		if current.Start.Line <= 0 || !positionBefore(current.Start, current.End) {
			index.invalid = true
			return index
		}
		for len(stack) > 0 && !positionBefore(current.Start, entries[stack[len(stack)-1]].location.End) {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			outer := entries[parent].location
			if !containsScoringRange(outer, current) || (sameScoringPosition(outer.Start, current.Start) && sameScoringPosition(outer.End, current.End)) {
				index.invalid = true
				return index
			}
			entries[i].parents = append(entries[i].parents, parent)
			for level := 1; len(entries[parent].parents) >= level; level++ {
				parent = entries[parent].parents[level-1]
				entries[i].parents = append(entries[i].parents, parent)
			}
		}
		stack = append(stack, i)
	}
	return index
}

func (index scoringRangeIndex) enclosing(location report.SourceRange) string {
	if index.invalid || location.Start.Line <= 0 {
		return ""
	}
	i := sort.Search(len(index.entries), func(i int) bool {
		return positionBefore(location.Start, index.entries[i].location.Start)
	}) - 1
	if i < 0 {
		return ""
	}
	if !containsScoringRange(index.entries[i].location, location) {
		for level := len(index.entries[i].parents) - 1; level >= 0; level-- {
			if level < len(index.entries[i].parents) {
				ancestor := index.entries[i].parents[level]
				if !containsScoringRange(index.entries[ancestor].location, location) {
					i = ancestor
				}
			}
		}
		if len(index.entries[i].parents) == 0 {
			return ""
		}
		i = index.entries[i].parents[0]
	}
	if containsScoringRange(index.entries[i].location, location) {
		return index.entries[i].key
	}
	return ""
}

func positionBefore(a, b report.SourcePosition) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Column < b.Column)
}

func sameScoringPosition(a, b report.SourcePosition) bool {
	return a.Line == b.Line && a.Column == b.Column
}

func containsScoringRange(outer, inner report.SourceRange) bool {
	return outer.Path != "" && outer.Path == inner.Path &&
		!positionBefore(inner.Start, outer.Start) && !positionBefore(outer.End, inner.End)
}
