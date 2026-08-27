package native

import (
	"sort"
	"strings"

	"github.com/blater/slopmochi/internal/analysiscache"
	"github.com/blater/slopmochi/internal/unitplan"
)

// dependencyGraphFingerprints computes one compact Merkle identity per unit.
// A unit is visited once; callers do not repeatedly materialize its complete
// transitive closure. Cycles are collapsed first so malformed or conservative
// project graphs remain deterministic and every member invalidates together.
func dependencyGraphFingerprints(units map[string]unitplan.Unit, localKeys map[string]analysiscache.Key) map[string]analysiscache.Key {
	ids := mapKeys(localKeys)
	index := 0
	indices := make(map[string]int, len(ids))
	lowlinks := make(map[string]int, len(ids))
	onStack := make(map[string]bool, len(ids))
	stack := make([]string, 0, len(ids))
	components := make([][]string, 0, len(ids))
	var connect func(string)
	connect = func(id string) {
		index++
		indices[id] = index
		lowlinks[id] = index
		stack = append(stack, id)
		onStack[id] = true
		for _, dependency := range units[id].DirectDependencies {
			if localKeys[dependency] == "" {
				continue
			}
			if indices[dependency] == 0 {
				connect(dependency)
				lowlinks[id] = min(lowlinks[id], lowlinks[dependency])
			} else if onStack[dependency] {
				lowlinks[id] = min(lowlinks[id], indices[dependency])
			}
		}
		if lowlinks[id] != indices[id] {
			return
		}
		component := []string{}
		for {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			component = append(component, last)
			if last == id {
				break
			}
		}
		sort.Strings(component)
		components = append(components, component)
	}
	for _, id := range ids {
		if indices[id] == 0 {
			connect(id)
		}
	}
	componentOf := componentMembership(components)
	componentKeys := make(map[int]analysiscache.Key, len(components))
	var fingerprintComponent func(int) analysiscache.Key
	fingerprintComponent = func(component int) analysiscache.Key {
		if key := componentKeys[component]; key != "" {
			return key
		}
		parts, dependencies := componentParts(components[component], units, localKeys, componentOf)
		dependencyComponents := sortedComponents(dependencies, components)
		for _, dependency := range dependencyComponents {
			parts = append(parts, "dependency:"+components[dependency][0]+"="+string(fingerprintComponent(dependency)))
		}
		key := analysiscache.Key(analysiscache.DigestBytes([]byte(strings.Join(parts, "\n"))))
		componentKeys[component] = key
		return key
	}
	result := make(map[string]analysiscache.Key, len(ids))
	for _, id := range ids {
		result[id] = fingerprintComponent(componentOf[id])
	}
	return result
}

func componentMembership(components [][]string) map[string]int {
	result := make(map[string]int)
	for component, members := range components {
		for _, id := range members {
			result[id] = component
		}
	}
	return result
}

func componentParts(members []string, units map[string]unitplan.Unit, localKeys map[string]analysiscache.Key, componentOf map[string]int) ([]string, map[int]bool) {
	parts := []string{"dependency-component-v1"}
	dependencies := map[int]bool{}
	component := componentOf[members[0]]
	for _, id := range members {
		parts = append(parts, "unit:"+id+"="+string(localKeys[id]))
		for _, dependency := range units[id].DirectDependencies {
			dependencyComponent, exists := componentOf[dependency]
			if exists && dependencyComponent != component {
				dependencies[dependencyComponent] = true
			}
		}
	}
	return parts, dependencies
}

func sortedComponents(dependencies map[int]bool, components [][]string) []int {
	result := make([]int, 0, len(dependencies))
	for dependency := range dependencies {
		result = append(result, dependency)
	}
	sort.Slice(result, func(i, j int) bool {
		return components[result[i]][0] < components[result[j]][0]
	})
	return result
}

func conservativeLanguageFingerprints(units map[string]unitplan.Unit, localKeys map[string]analysiscache.Key) map[string]analysiscache.Key {
	parts := map[string][]string{}
	for id, key := range localKeys {
		language := string(units[id].Language)
		parts[language] = append(parts[language], id+"="+string(key))
	}
	result := make(map[string]analysiscache.Key, len(parts))
	for language, values := range parts {
		sort.Strings(values)
		result[language] = analysiscache.Key(analysiscache.DigestBytes([]byte("conservative-language-v1\n" + strings.Join(values, "\n"))))
	}
	return result
}
