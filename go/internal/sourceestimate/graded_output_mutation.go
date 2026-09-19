package sourceestimate

func gradedOutputMutation(body []token, kinds map[string]string, outputs map[string]bool, root string, i int) {
	if i+1 < len(body) && body[i+1].text == "[" {
		end := matching(body, i+1, "[", "]")
		if end > i && end+1 < len(body) && (body[end+1].text == "=" || body[end+1].text == "+=") {
			outputs[root] = true
		}
	}
	if i+3 < len(body) && body[i+1].text == "." && body[i+3].text == "(" {
		method, kind := body[i+2].text, kinds[root]
		if kind == "vector" && (method == "push" || method == "extend" || method == "insert" || method == "clear") || kind == "array" && (method == "push" || method == "splice") {
			outputs[root] = true
		}
	}
}
