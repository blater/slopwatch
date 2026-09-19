package sourceestimate

func gradedRustExpressionFlow(expression []token, fields map[string]gradedRustFlow, locals map[string]gradedRustFlow) gradedRustFlow {
	empty := gradedRustFlow{}
	for len(expression) > 1 && expression[0].text == "(" && matching(expression, 0, "(", ")") == len(expression)-1 {
		expression = expression[1 : len(expression)-1]
	}
	if len(expression) == 0 {
		return empty
	}
	flow, ok := locals[expression[0].text]
	if !ok || flow.kind == "" {
		return empty
	}
	i := 1
	for i < len(expression) {
		if expression[i].text != "." || i+1 >= len(expression) {
			return empty
		}
		name := expression[i+1].text
		i += 2
		if flow.kind == "owner" {
			field, ok := fields[name]
			if !ok {
				return empty
			}
			flow = field
			continue
		}
		if i+1 < len(expression) && expression[i].text == "::" && expression[i+1].text == "<" {
			end := rustGenericEnd(expression, i+1)
			if end < 0 {
				return empty
			}
			i = end + 1
		}
		if i >= len(expression) || expression[i].text != "(" {
			return empty
		}
		end := matching(expression, i, "(", ")")
		if end < 0 {
			return empty
		}
		args := expression[i+1 : end]
		switch flow.kind {
		case "pointer":
			if name != "add" && name != "sub" && name != "offset" && name != "wrapping_add" && name != "wrapping_sub" && name != "cast" && name != "cast_slice" {
				return empty
			}
		case "nonnull":
			if name == "as_ptr" {
				flow.kind = "pointer"
			} else if (name == "as_mut" || name == "as_ref") && flow.inner != "" {
				flow.kind = flow.inner
			} else {
				return empty
			}
		case "vec":
			if name == "as_ptr" || name == "as_mut_ptr" {
				flow.kind = "pointer"
			} else if name == "as_slice" {
				flow.kind = "slice"
			} else {
				return empty
			}
		case "slice":
			if name == "as_ptr" || name == "as_mut_ptr" {
				flow.kind = "pointer"
			} else {
				return empty
			}
		case "iter":
			if name == "as_slice" {
				flow.kind = "slice"
			} else {
				return empty
			}
		default:
			return empty
		}
		offset := name == "add" || name == "sub" || name == "offset" || name == "wrapping_add" || name == "wrapping_sub"
		if name != "cast" && name != "cast_slice" && !(offset && joinTokens(args) == "0") {
			flow.identity += "." + name + "(" + joinTokens(args) + ")"
		}
		if len(flow.identity) > 2048 {
			return empty
		}
		i = end + 1
	}
	return flow
}
