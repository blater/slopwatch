package sourceestimate

import (
	"path/filepath"
	"strings"
)

type sourceImport struct {
	file, name, pkg string
	index           int
}

func annotateTypeScriptImports(units []unit) {
	paths := map[string]string{}
	indices := map[string]int{}
	packages := map[string]string{}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) == "typescript" {
			paths[filepath.ToSlash(filepath.Clean(u.file.Path))] = u.file.Path
			indices[u.file.Path] = u.index
			packages[u.file.Path] = u.pkg
		}
	}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "typescript" {
			continue
		}
		imports := map[string]sourceImport{}
		for i := 0; i < len(u.tokens); i++ {
			if u.tokens[i].text != "import" {
				continue
			}
			start := i + 1
			end := start
			for end < len(u.tokens) && u.tokens[end].text != "from" && u.tokens[end].text != ";" {
				end++
			}
			if end+1 >= len(u.tokens) || u.tokens[end].text != "from" {
				continue
			}
			spec := strings.Trim(u.tokens[end+1].literal, "\"'")
			if !strings.HasPrefix(spec, ".") {
				continue
			}
			base := filepath.ToSlash(filepath.Join(filepath.Dir(u.file.Path), spec))
			target := ""
			for _, candidate := range []string{base, base + ".ts", base + ".tsx", base + "/index.ts", strings.TrimSuffix(base, ".js") + ".ts"} {
				if found := paths[candidate]; found != "" {
					target = found
					break
				}
			}
			if target == "" {
				continue
			}
			if start+2 < end && u.tokens[start].text == "*" && u.tokens[start+1].text == "as" {
				imports[u.tokens[start+2].text] = sourceImport{file: target, name: "*", pkg: packages[target], index: indices[target]}
				continue
			}
			if start >= end || u.tokens[start].text != "{" {
				continue
			}
			for j := start + 1; j < end && u.tokens[j].text != "}"; j++ {
				if !isIdentifier(u.tokens[j].text) {
					continue
				}
				name, alias := u.tokens[j].text, u.tokens[j].text
				if j+2 < end && u.tokens[j+1].text == "as" {
					alias = u.tokens[j+2].text
					j += 2
				}
				imports[alias] = sourceImport{file: target, name: name, pkg: packages[target], index: indices[target]}
			}
		}
		for _, op := range u.ops {
			op.imports = imports
		}
	}
}

func resolveTypeScriptImportedCall(caller *operation, c call, units []unit, byKey *operationLookup) (callSelection, bool) {
	if caller.language != "typescript" {
		return callSelection{}, false
	}
	alias, member := c.name, ""
	if dot := strings.IndexByte(alias, '.'); dot >= 0 {
		alias, member = alias[:dot], alias[dot+1:]
	}
	binding, ok := caller.imports[alias]
	if !ok {
		return callSelection{}, false
	}
	name := binding.name
	if name == "*" {
		name = member
	}
	if name == "" || strings.Contains(name, ".") {
		return callSelection{}, true
	}
	lookup := *caller
	lookup.file = binding.index
	lookup.pkg = binding.pkg
	owner := ""
	if member != "" && binding.name != "*" {
		owner, name = binding.name, member
	}
	if binding.index < 0 || binding.index >= len(units) || units[binding.index].file.Path != binding.file {
		return callSelection{}, true
	}
	return callSelection{bucket: byKey.exposed[scopedOperationKey(&lookup, name, owner)]}, true
}
