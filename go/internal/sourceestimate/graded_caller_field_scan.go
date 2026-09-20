package sourceestimate

type gradedCallerRefScan struct {
	u              unit
	operation      *operation
	bindings       map[string]string
	resolver       gradedCallerResolver
	controlled     map[string]bool
	transitions    map[string]map[string]map[string]bool
	index          gradedCallerBodyIndex
	argumentRefs   map[string]bool
	predicateStart map[int]int
	epochs         map[string]int
	refs           map[string]map[string]fieldRef
	writes         map[string]map[string]map[string]bool
	conditions     map[string]map[string]bool
}

func newGradedCallerRefScan(u unit, op *operation, bindings map[string]string, resolver gradedCallerResolver, controlled map[string]bool, transitions map[string]map[string]map[string]bool) *gradedCallerRefScan {
	index := indexGradedCallerBody(op.body)
	return &gradedCallerRefScan{
		u: u, operation: op, bindings: bindings, resolver: resolver,
		controlled: controlled, transitions: transitions, index: index,
		argumentRefs:   gradedCallerArgumentRefsIndexed(op.body, bindings, index),
		predicateStart: gradedCallerPredicateRefsIndexed(op.body, index),
		epochs:         map[string]int{}, refs: map[string]map[string]fieldRef{},
		writes: map[string]map[string]map[string]bool{}, conditions: map[string]map[string]bool{},
	}
}

func (scan *gradedCallerRefScan) run() {
	for i := range scan.operation.body {
		scan.observeStorageWrite(i)
		scan.inspectMember(i)
	}
}

func (scan *gradedCallerRefScan) inspectMember(position int) {
	receiver, name, ok := scan.memberAt(position)
	if !ok {
		return
	}
	typ, ok := scan.receiverType(position, receiver)
	if !ok {
		return
	}
	ref, ok := scan.resolvedField(typ, name)
	if !ok {
		return
	}
	scan.ensureReceiver(receiver)
	scan.refs[receiver][name] = ref
	scan.recordRead(receiver, name)
	scan.recordAssignment(position, receiver, name, ref)
	scan.recordPredicate(position, receiver, name)
}

func (scan *gradedCallerRefScan) memberAt(position int) (string, string, bool) {
	if position < 2 || scan.operation.body[position-1].text != "." {
		return "", "", false
	}
	return scan.operation.body[position-2].text, scan.operation.body[position].text, true
}

func (scan *gradedCallerRefScan) receiverType(position int, receiver string) (string, bool) {
	typ := scan.bindings[receiver]
	if typ == "" {
		return "", false
	}
	if position < 4 || scan.operation.body[position-3].text != "." {
		return typ, true
	}
	if scan.operation.body[position-4].text != "this" {
		return "", false
	}
	typ = callerDeclaredFields(scan.u, scan.operation.owner)[receiver].typeName
	return typ, typ != ""
}

func (scan *gradedCallerRefScan) resolvedField(typ, name string) (fieldRef, bool) {
	ref, ok := scan.resolver.resolve(scan.u.pkg, typ, name)
	if !ok || ref.owner == scan.operation.owner || !ref.field.mutable || (!ref.field.public && !ref.field.packageVisible) {
		return fieldRef{}, false
	}
	return ref, true
}

func (scan *gradedCallerRefScan) ensureReceiver(receiver string) {
	if scan.refs[receiver] != nil {
		return
	}
	scan.refs[receiver] = map[string]fieldRef{}
	scan.writes[receiver] = map[string]map[string]bool{}
	scan.conditions[receiver] = map[string]bool{}
}

func (scan *gradedCallerRefScan) recordRead(receiver, name string) {
	if scan.argumentRefs[receiver+"."+name] {
		scan.conditions[receiver][name] = true
	}
}

func (scan *gradedCallerRefScan) recordPredicate(position int, receiver, name string) {
	start, ok := scan.predicateStart[position]
	if ok && gradedCallerBranchEffect(scan.operation.body, start, receiver) {
		scan.conditions[receiver][name] = true
	}
}
