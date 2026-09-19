package sourceestimate

type gradedCallerResolver struct {
	types map[string][]gradedCallerType
}

func (r gradedCallerResolver) resolve(pkg, typ, name string) (fieldRef, bool) {
	seen := map[string]bool{}
	for depth := 0; depth < maxCallDepth; depth++ {
		key := pkg + "#" + typ
		if seen[key] || len(r.types[key]) != 1 {
			return fieldRef{}, false
		}
		seen[key] = true
		d := r.types[key][0]
		if f, ok := d.fields[name]; ok {
			return fieldRef{d.owner, f, d.unit}, true
		}
		if d.parent == "" {
			return fieldRef{}, false
		}
		typ = d.parent
	}
	return fieldRef{}, false
}
