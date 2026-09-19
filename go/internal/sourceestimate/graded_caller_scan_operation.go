package sourceestimate

func gradedCallerScanOperation(u unit, op0 *operation, units []unit, byKey map[string][]*operation, resolver gradedCallerResolver, controlled map[string]bool, transitions map[string]map[string]map[string]bool, record func(fieldRef, *operation)) []consumerRecord {
	consumers := []consumerRecord{}
	op := op0

	copy := *op
	copy.body = gradedEagerBody(pruneDeadFalseBranches(op.body), op.language)
	op = &copy

	for i, t := range op.body {
		if t.text == "return" && gradedUnconditional(op.body, i) {
			op.body = op.body[:statementEnd(op.body, i)]
			break
		}
	}
	bindings := map[string]string{}
	for name, f := range callerDeclaredFields(u, op.owner) {
		bindings[name] = f.typeName
	}
	for name, typ := range op.parameterTypes {
		bindings[name] = typ
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
	consumers = append(consumers, gradedCallerCollectConsumers(u, op, units, byKey, resolver, bindings)...)
	refs, writes, _ := gradedCallerScanRefs(u, op, bindings, resolver, controlled, transitions)
	for receiver, fields := range refs {
		for name := range writes[receiver] {
			record(fields[name], op)
		}
	}

	return consumers
}
