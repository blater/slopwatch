package sourceestimate

// Go operations share the immutable source field inventory and carry only their
// receiver/local overrides. Contextual bodies retain the complete lookup space.
func operationFieldType(op *operation, name string) (string, bool) {
	for layer := op.fieldTypeContext; layer != nil; layer = layer.previous {
		if typ, ok := layer.parameters[name]; ok {
			return typ, true
		}
		if field, ok := layer.fields[name]; ok {
			return field.typeName, true
		}
	}
	if typ, ok := op.fieldTypeBindings[name]; ok {
		return typ, true
	}
	typ, ok := op.fieldTypes[name]
	return typ, ok
}

// Context layers borrow immutable maps and retain earlier lookup overrides.
// Empty type values still shadow lower layers, just as a materialized map does.
type operationTypeContext struct {
	parameters map[string]string
	fields     map[string]gradedSurfaceField
	previous   *operationTypeContext
}
