package native

import "github.com/blater/slopwatch/internal/unitplan"

type changeLookup interface {
	unitplan.Lookup
	Reverse(string) []string
}

func affectedIndexedUnits(previous, current changeLookup, changes map[string]*unitplan.Source) map[string]bool {
	affected := map[string]bool{}
	languages := map[unitplan.Language]bool{}
	for path, next := range changes {
		for _, id := range previous.Consumers(path) {
			affected[id] = true
		}
		for _, id := range current.Consumers(path) {
			affected[id] = true
		}
		if next != nil {
			languages[next.Language] = true
		}
		if old, ok := previous.Source(path); ok {
			languages[old.Language] = true
		}
	}
	for language := range languages {
		for _, id := range previous.Conservative(language) {
			affected[id] = true
		}
		for _, id := range current.Conservative(language) {
			affected[id] = true
		}
	}
	queue := mapKeys(affected)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, view := range []changeLookup{previous, current} {
			for _, dependent := range view.Reverse(id) {
				if !affected[dependent] {
					affected[dependent] = true
					queue = append(queue, dependent)
				}
			}
		}
	}
	return affected
}
func indexedVisibleUnits(view unitplan.Lookup, affected map[string]bool, options Options) ([]plannedCacheUnit, map[string]bool) {
	var units []plannedCacheUnit
	paths := map[string]bool{}
	for id := range affected {
		unit, ok := view.Unit(id)
		if !ok || !eligibleUnit(unit, options) {
			continue
		}
		unit = effectiveChangeUnit(unit, options)
		owned := []string{}
		visible := false
		for _, path := range unit.Sources {
			if canonicalOwner(view, path, options) != id {
				continue
			}
			owned = append(owned, path)
			if visibleSource(options, path, string(unit.Language)) {
				paths[path] = true
				visible = true
			}
		}
		if visible {
			units = append(units, plannedCacheUnit{plan: unit, owned: owned})
		}
	}
	return units, paths
}
func indexedRequestClosure(view unitplan.Lookup, active []plannedCacheUnit, options Options) map[string]unitplan.Unit {
	selected := map[string]unitplan.Unit{}
	queue := []string{}
	broadened := map[unitplan.Language]bool{}
	for _, unit := range active {
		queue = append(queue, unit.plan.ID)
		if unit.plan.Conservative && !broadened[unit.plan.Language] {
			broadened[unit.plan.Language] = true
			queue = append(queue, view.Group(unit.plan.Language)...)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, ok := selected[id]; ok {
			continue
		}
		unit, ok := view.Unit(id)
		if !ok || !eligibleUnit(unit, options) {
			continue
		}
		unit = effectiveChangeUnit(unit, options)
		selected[id] = unit
		queue = append(queue, unit.DirectDependencies...)
	}
	return selected
}
