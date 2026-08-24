package metrics

import (
	"sort"

	"slopslap.dev/structural/internal/facts"
)

func deepIf(statements []*facts.Statement, depth int, output *[]facts.Location) {
	for _, statement := range statements {
		if statement.Kind != facts.StmtIf {
			deepIf(statement.Body, depth, output)
			for _, branch := range statement.Cases {
				deepIf(branch.Body, depth, output)
			}
			continue
		}
		if depth+1 == 3 {
			*output = append(*output, statement.Location)
			continue
		}
		deepIf(statement.Body, depth+1, output)
		if len(statement.Else) == 1 && statement.Else[0].Kind == facts.StmtIf {
			deepIf(statement.Else, depth, output)
		} else {
			deepIf(statement.Else, depth+1, output)
		}
	}
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func typeWMC(item *facts.Type) int {
	return typeWMCWith(item, Cyclomatic)
}

func typeWMCWith(item *facts.Type, cyclomatic func(*facts.Function) int) int {
	value := item.InterfaceMethodCount
	for _, method := range item.Methods {
		value += cyclomatic(method)
	}
	return value
}

func tcc(item *facts.Type) float64 {
	methods := make([]string, 0, len(item.MethodFields))
	for method := range item.MethodFields {
		methods = append(methods, method)
	}
	if len(methods) < 2 {
		return 0
	}
	methodsByField := make(map[string][]int)
	for index, method := range methods {
		for _, field := range item.MethodFields[method] {
			methodsByField[field] = append(methodsByField[field], index)
		}
	}
	connected := 0
	for left := 0; left < len(methods); left++ {
		connectedRights := make(map[int]struct{})
		for _, field := range item.MethodFields[methods[left]] {
			for _, right := range methodsByField[field] {
				if right > left {
					connectedRights[right] = struct{}{}
				}
			}
		}
		connected += len(connectedRights)
	}
	pairs := len(methods) * (len(methods) - 1) / 2
	return float64(connected) / float64(pairs)
}
