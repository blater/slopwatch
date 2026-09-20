package sourceestimate

// gradedCallerScanRefs coordinates the field-effect scan while keeping its
// result shape stable for the caller analysis pipeline.
func gradedCallerScanRefs(u unit, op *operation, bindings map[string]string, resolver gradedCallerResolver, controlled map[string]bool, transitions map[string]map[string]map[string]bool) (map[string]map[string]fieldRef, map[string]map[string]map[string]bool, map[string]map[string]bool) {
	scan := newGradedCallerRefScan(u, op, bindings, resolver, controlled, transitions)
	scan.run()
	return scan.refs, scan.writes, scan.conditions
}
