package sourceestimate

import "strings"

func gradedCallerScanOperation(u unit, op0 *operation, units []unit, byKey *operationLookup, resolver gradedCallerResolver, controlled map[string]bool, transitions map[string]map[string]map[string]bool, record func(fieldRef, *operation)) []consumerRecord {
	consumers := []consumerRecord{}
	op := op0

	copy := *op
	copy.body = normalizedEagerBody(op)
	op = &copy

	for i, t := range op.body {
		if t.text == "return" && normalizedEagerUnconditional(op0, i) {
			op.body = op.body[:statementEnd(op.body, i)]
			break
		}
	}
	// Consumer dependencies can name a receiver used only inside a callee.
	// Resolve just names needed by either witness source against shared maps.
	constraints := gradedConsumerConstraints(op, u, units, byKey, map[string]map[string]bool{}, 0)
	names := map[string]bool{}
	for _, tok := range op.body {
		names[tok.text] = true
	}
	for _, consumer := range constraints {
		for dep := range consumer.fields {
			if dot := strings.IndexByte(dep, '.'); dot >= 0 {
				names[dep[:dot]] = true
			}
		}
	}
	bindings := map[string]string{}
	fields := callerDeclaredFields(u, op.owner)
	for name := range names {
		if typ, ok := op.parameterTypes[name]; ok {
			bindings[name] = typ
		} else if field, ok := fields[name]; ok {
			bindings[name] = field.typeName
		}
	}
	// A local declaration shadows a field/parameter; ambiguous inferred or
	// assigned aliases are deliberately excluded from this bounded proof.
	for i := 1; i+1 < len(op.body); i++ {
		if isIdentifier(op.body[i].text) && isIdentifier(op.body[i-1].text) && (op.body[i+1].text == "=" || op.body[i+1].text == ";") {
			delete(bindings, op.body[i].text)
		}
	}
	for i, t := range op.body {
		if _, ok := bindings[t.text]; ok && i+1 < len(op.body) && op.body[i+1].text == "=" && (i == 0 || op.body[i-1].text != "." || i >= 2 && op.body[i-2].text == "this") {
			delete(bindings, t.text)
		}
	}
	consumers = append(consumers, gradedCallerCollectKnownConsumers(u, op, units, resolver, bindings, constraints)...)
	refs, writes, _ := gradedCallerScanRefs(u, op, bindings, resolver, controlled, transitions)
	for receiver, fields := range refs {
		for name := range writes[receiver] {
			record(fields[name], op)
		}
	}

	return consumers
}
