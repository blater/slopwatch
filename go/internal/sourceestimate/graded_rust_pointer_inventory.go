package sourceestimate

import "strings"

func gradedRustPointerFields(u unit, owner string) map[string]gradedRustFlow {
	result := map[string]gradedRustFlow{}
	decl := gradedFindSurfaceOwner(u.tokens, "rust", owner)
	if decl == nil {
		return result
	}
	for i := decl.open + 1; i+2 < decl.close; i++ {
		if u.tokens[i+1].text != ":" {
			continue
		}
		name := u.tokens[i].text
		if _, ok := decl.fields[name]; !ok {
			continue
		}
		end := i + 2
		depth := 0
		for end < decl.close {
			switch u.tokens[end].text {
			case "<":
				depth++
			case ">":
				depth--
			case ">>":
				depth -= 2
			}
			if u.tokens[end].text == "," && depth == 0 {
				break
			}
			end++
		}
		typ := u.tokens[i+2 : end]
		shadowed := false
		for j := 0; j+2 < decl.open; j++ {
			if u.tokens[j].text == "struct" && u.tokens[j+1].text == owner && u.tokens[j+2].text == "<" {
				for name := range gradedRustGenericBindings(u.tokens[j+2 : decl.open]) {
					if len(typ) > 0 && typ[0].text == name {
						shadowed = true
					}
				}
			}
		}
		if shadowed {
			continue
		}
		kind, inner := gradedRustPointerType(typ, u.tokens)
		if kind != "" {
			result[name] = gradedRustFlow{kind: kind, inner: inner, identity: "self." + name, fields: map[string]bool{name: true}}
		}
	}
	return result
}
func gradedRustPointerType(typ, source []token) (kind, inner string) {
	if len(typ) > 0 && typ[0].text == "*" {
		return "pointer", ""
	}
	name := rustImplPathName(typ)
	pathEnd := len(typ)
	for i, t := range typ {
		if t.text == "<" {
			pathEnd = i
			break
		}
	}
	path := joinTokens(typ[:pathEnd])
	qualified := strings.Contains(path, "::")
	if qualified && path != "core::ptr::NonNull" && path != "std::ptr::NonNull" && path != "std::vec::Vec" && path != "alloc::vec::Vec" {
		return "", ""
	}
	if name == "NonNull" && (qualified || gradedRustImportedType(source, "NonNull", []string{"core::ptr", "std::ptr"})) {
		kind = "nonnull"
		for i, t := range typ {
			if t.text == "<" {
				end := rustGenericEnd(typ, i)
				if end > i {
					inner, _ = gradedRustPointerType(typ[i+1:end], source)
				}
				break
			}
		}
		return
	}
	if name == "Vec" && (qualified || gradedRustImportedType(source, "Vec", []string{"std::vec", "alloc::vec"})) {
		return "vec", ""
	}
	return "", ""
}
