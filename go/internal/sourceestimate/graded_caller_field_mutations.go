package sourceestimate

func (scan *gradedCallerRefScan) observeStorageWrite(position int) {
	if position+1 >= len(scan.operation.body) || !gradedStorageWriteAt(scan.operation.body, position) {
		return
	}
	start := position
	for start >= 2 && scan.operation.body[start-1].text == "." {
		start -= 2
	}
	key := joinTokens(scan.operation.body[start : position+1])
	scan.epochs[key]++
}

func (scan *gradedCallerRefScan) recordAssignment(position int, receiver, name string, ref fieldRef) {
	if position+1 >= len(scan.operation.body) || scan.operation.body[position+1].text != "=" {
		return
	}
	scan.controlled[fieldRefKey(ref, name)] = true
	end := statementEnd(scan.operation.body, position+2)
	scan.writes[receiver][name] = gradedCallerDependencyVersions(scan.operation.body[position+2:end], scan.epochs)
	scan.recordBooleanTransition(position, ref, name)
}

func fieldRefKey(ref fieldRef, name string) string {
	return itoa(ref.unit) + "#" + ref.owner + "#" + name
}

func (scan *gradedCallerRefScan) recordBooleanTransition(position int, ref fieldRef, name string) {
	if !gradedExactBooleanAssignment(scan.operation.body, position, "true") && !gradedExactBooleanAssignment(scan.operation.body, position, "false") {
		return
	}
	key := fieldRefKey(ref, name)
	if scan.transitions[key] == nil {
		scan.transitions[key] = map[string]map[string]bool{}
	}
	state := scan.operation.body[position+2].text
	if scan.transitions[key][state] == nil {
		scan.transitions[key][state] = map[string]bool{}
	}
	scan.transitions[key][state][scan.operation.id] = true
}
