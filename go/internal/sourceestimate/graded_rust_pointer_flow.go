package sourceestimate

import "strings"

func gradedRustPointerContracts(tokens []token) map[string]bool {
	for i, t := range tokens {
		if t.text == "mod" && i+1 < len(tokens) && (tokens[i+1].text == "core" || tokens[i+1].text == "std") {
			return nil
		}
	}
	contracts := map[string]bool{"core.ptr.copy": true, "std.ptr.copy": true, "core.ptr.drop_in_place": true, "std.ptr.drop_in_place": true}
	for i, t := range tokens {
		if t.text != "use" {
			continue
		}
		end := i + 1
		for end < len(tokens) && tokens[end].text != ";" {
			end++
		}
		path := joinTokens(tokens[i+1 : end])
		for _, base := range []string{"core::ptr", "std::ptr"} {
			if path == base || strings.HasPrefix(path, base+"::{self,") {
				contracts["ptr.copy"] = true
				contracts["ptr.drop_in_place"] = true
			}
			if strings.HasPrefix(path, base+"as") {
				alias := strings.TrimPrefix(path, base+"as")
				contracts[alias+".copy"] = true
				contracts[alias+".drop_in_place"] = true
			}
		}
	}
	for i, t := range tokens {
		if t.text != "use" {
			continue
		}
		end := i + 1
		for end < len(tokens) && tokens[end].text != ";" {
			end++
		}
		path := joinTokens(tokens[i+1 : end])
		if strings.HasPrefix(path, "core::ptr") || strings.HasPrefix(path, "std::ptr") {
			continue
		}
		for key := range contracts {
			bound := strings.Split(key, ".")[0]
			if bound == "core" || bound == "std" {
				continue
			}
			for _, part := range tokens[i+1 : end] {
				if part.text == bound {
					delete(contracts, key)
				}
			}
		}
	}
	// A conflicting local module or function keeps the purported library binding
	// unresolved rather than awarding credit from its spelling.
	for i, t := range tokens {
		if (t.text == "mod" || t.text == "fn") && i+1 < len(tokens) {
			name := tokens[i+1].text
			for key := range contracts {
				if key == name || strings.HasPrefix(key, name+".") {
					delete(contracts, key)
				}
			}
		}
	}
	return contracts
}
