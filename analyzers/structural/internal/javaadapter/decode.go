package javaadapter

import (
	"bufio"
	"fmt"

	"slopslap.dev/structural/internal/facts"
)

func readTypes(data *bufio.Reader) ([]*facts.Type, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]*facts.Type, count)
	for index := range result {
		item, itemErr := readType(data)
		if itemErr != nil {
			return nil, itemErr
		}
		result[index] = item
	}
	return result, nil
}

func readType(data *bufio.Reader) (*facts.Type, error) {
	item := &facts.Type{}
	var err error
	if item.Name, err = readString(data); err != nil {
		return nil, err
	}
	if item.Kind, err = readString(data); err != nil {
		return nil, err
	}
	if item.Location, err = readLocation(data); err != nil {
		return nil, err
	}
	if item.MethodLocations, err = readLocations(data); err != nil {
		return nil, err
	}
	methods, err := readUint32(data)
	if err != nil {
		return nil, err
	}
	item.InterfaceMethodCount = int(methods)
	if item.ForeignTypes, err = readStrings(data); err != nil {
		return nil, err
	}
	if item.MethodFields, err = readMethodFields(data); err != nil {
		return nil, err
	}
	if item.ForeignFields, err = readStrings(data); err != nil {
		return nil, err
	}
	return item, nil
}

func readLocations(data *bufio.Reader) ([]facts.Location, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]facts.Location, count)
	for index := range result {
		if result[index], err = readLocation(data); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readShape(data *bufio.Reader) (*facts.TypeShape, error) {
	shape := &facts.TypeShape{}
	var err error
	if shape.StableID, err = readString(data); err != nil {
		return nil, err
	}
	if shape.Kind, err = readString(data); err != nil {
		return nil, err
	}
	if shape.Name, err = readString(data); err != nil {
		return nil, err
	}
	if shape.Children, err = readShapes(data); err != nil {
		return nil, err
	}
	if shape.ExposedMembers, err = readStrings(data); err != nil {
		return nil, err
	}
	value, err := readUint32(data)
	if err != nil {
		return nil, err
	}
	shape.Complexity = int(value)
	return shape, nil
}

func readShapes(data *bufio.Reader) ([]*facts.TypeShape, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]*facts.TypeShape, count)
	for index := range result {
		result[index], err = readShape(data)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readOperations(data *bufio.Reader) ([]*facts.PublicOperation, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]*facts.PublicOperation, count)
	for index := range result {
		item := &facts.PublicOperation{}
		if item.StableID, err = readString(data); err != nil {
			return nil, err
		}
		if item.Name, err = readString(data); err != nil {
			return nil, err
		}
		if item.OwnerType, err = readString(data); err != nil {
			return nil, err
		}
		if item.Location, err = readLocation(data); err != nil {
			return nil, err
		}
		if item.Parameters, err = readShapes(data); err != nil {
			return nil, err
		}
		if item.Results, err = readShapes(data); err != nil {
			return nil, err
		}
		if item.EmitsOutput, err = readBool(data); err != nil {
			return nil, err
		}
		if item.ObservableMutation, err = readBool(data); err != nil {
			return nil, err
		}
		result[index] = item
	}
	return result, nil
}

func readBool(data *bufio.Reader) (bool, error) {
	value, err := data.ReadByte()
	if err != nil {
		return false, err
	}
	if value > 1 {
		return false, fmt.Errorf("invalid Java boolean %d", value)
	}
	return value == 1, nil
}

func readExposures(data *bufio.Reader) ([]*facts.RepresentationExposure, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]*facts.RepresentationExposure, count)
	for index := range result {
		item := &facts.RepresentationExposure{}
		if item.StableID, err = readString(data); err != nil {
			return nil, err
		}
		if item.Kind, err = readString(data); err != nil {
			return nil, err
		}
		if item.Entity, err = readString(data); err != nil {
			return nil, err
		}
		if item.Location, err = readLocation(data); err != nil {
			return nil, err
		}
		if item.Evidence, err = readString(data); err != nil {
			return nil, err
		}
		if item.Confidence, err = readString(data); err != nil {
			return nil, err
		}
		result[index] = item
	}
	return result, nil
}

func readMethodFields(data *bufio.Reader) (map[string][]string, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string, count)
	for index := 0; index < count; index++ {
		key, keyErr := readString(data)
		if keyErr != nil {
			return nil, keyErr
		}
		values, valuesErr := readStrings(data)
		if valuesErr != nil {
			return nil, valuesErr
		}
		result[key] = values
	}
	return result, nil
}

func readStatements(data *bufio.Reader) ([]*facts.Statement, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]*facts.Statement, count)
	for index := range result {
		if result[index], err = readStatement(data); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readStatement(data *bufio.Reader) (*facts.Statement, error) {
	kind, err := data.ReadByte()
	if err != nil {
		return nil, err
	}
	if kind > byte(facts.StmtGoto) {
		return nil, fmt.Errorf("invalid Java statement kind %d", kind)
	}
	result := &facts.Statement{Kind: facts.StmtKind(kind)}
	if result.Location, err = readLocation(data); err != nil {
		return nil, err
	}
	if result.Condition, err = readOptionalExpression(data); err != nil {
		return nil, err
	}
	if result.Expressions, err = readExpressions(data); err != nil {
		return nil, err
	}
	if result.Body, err = readStatements(data); err != nil {
		return nil, err
	}
	if result.Else, err = readStatements(data); err != nil {
		return nil, err
	}
	if result.Cases, err = readCases(data); err != nil {
		return nil, err
	}
	if result.MaySkip, err = readBoolean(data, "loop"); err != nil {
		return nil, err
	}
	if result.Labeled, err = readBoolean(data, "label"); err != nil {
		return nil, err
	}
	return result, nil
}

func readOptionalExpression(data *bufio.Reader) (*facts.Expression, error) {
	present, err := data.ReadByte()
	if err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	if present != 1 {
		return nil, fmt.Errorf("invalid Java condition status")
	}
	return readExpression(data)
}

func readCases(data *bufio.Reader) ([]facts.Case, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]facts.Case, count)
	for index := range result {
		if result[index], err = readCase(data); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readCase(data *bufio.Reader) (facts.Case, error) {
	result := facts.Case{}
	var err error
	if result.Default, err = readBoolean(data, "case"); err != nil {
		return result, err
	}
	if result.FallsThrough, err = readBoolean(data, "fallthrough"); err != nil {
		return result, err
	}
	if result.Expressions, err = readExpressions(data); err != nil {
		return result, err
	}
	if result.Body, err = readStatements(data); err != nil {
		return result, err
	}
	return result, nil
}

func readExpressions(data *bufio.Reader) ([]*facts.Expression, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]*facts.Expression, count)
	for index := range result {
		if result[index], err = readExpression(data); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readExpression(data *bufio.Reader) (*facts.Expression, error) {
	kind, err := data.ReadByte()
	if err != nil {
		return nil, err
	}
	if kind > byte(facts.ExprConditional) {
		return nil, fmt.Errorf("invalid Java expression kind %d", kind)
	}
	result := &facts.Expression{Kind: facts.ExprKind(kind)}
	if result.Children, err = readExpressions(data); err != nil {
		return nil, err
	}
	if result.Calls, err = readStrings(data); err != nil {
		return nil, err
	}
	if result.Nested, err = readFunctions(data); err != nil {
		return nil, err
	}
	return result, nil
}

func readStrings(data *bufio.Reader) ([]string, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]string, count)
	for index := range result {
		if result[index], err = readString(data); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readLocation(data *bufio.Reader) (facts.Location, error) {
	result := facts.Location{}
	var err error
	if result.Path, err = readString(data); err != nil {
		return result, err
	}
	values := []*int{&result.Line, &result.Column, &result.EndLine, &result.EndColumn}
	for _, target := range values {
		value, valueErr := readUint32(data)
		if valueErr != nil {
			return result, valueErr
		}
		*target = int(value)
	}
	return result, nil
}

func readBoolean(data *bufio.Reader, name string) (bool, error) {
	value, err := data.ReadByte()
	if err != nil {
		return false, err
	}
	if value > 1 {
		return false, fmt.Errorf("invalid Java %s status", name)
	}
	return value == 1, nil
}
