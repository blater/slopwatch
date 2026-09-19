package sourceestimate

import "strings"

func gradedJavaLibraryType(u unit, typ, pkg string) bool {
	if normalizeLanguage(u.file.Language, u.file.Path) != "java" {
		return false
	}
	for i, t := range u.tokens {
		if (t.text == "class" || t.text == "interface" || t.text == "enum" || t.text == "record") && i+1 < len(u.tokens) && u.tokens[i+1].text == typ {
			return false
		}
		if t.text == typ && i > 0 && i+1 < len(u.tokens) && (u.tokens[i-1].text == "<" || u.tokens[i-1].text == ",") && (u.tokens[i+1].text == "extends" || u.tokens[i+1].text == ">") {
			return false
		}
	}
	wildcard, explicit := false, false
	for i, t := range u.tokens {
		if t.text != "import" {
			continue
		}
		end := i + 1
		for end < len(u.tokens) && u.tokens[end].text != ";" {
			end++
		}
		name := joinTokens(u.tokens[i+1 : end])
		if strings.HasSuffix(name, "."+typ) {
			if name != pkg+"."+typ {
				return false
			}
			explicit = true
		}
		wildcard = wildcard || name == pkg+".*"
	}
	if !explicit && u.packageTypes[typ] {
		return false
	}
	return explicit || wildcard || pkg == "java.lang"
}
func gradedMapType(u unit, typ string) bool {
	return (typ == "Map" || typ == "HashMap" || typ == "IdentityHashMap" || typ == "LinkedHashMap") && gradedJavaLibraryType(u, typ, "java.util")
}
func gradedJDBCCatchType(u unit, header []token) bool {
	// Remove the variable declarator, then resolve complete exception type names
	// in the union. Variable names never supply a type contract.
	if len(header) < 2 {
		return false
	}
	types := header[:len(header)-1]
	start := 0
	for end := 0; end <= len(types); end++ {
		if end < len(types) && types[end].text != "|" {
			continue
		}
		name := joinTokens(types[start:end])
		if name == "java.sql.SQLException" || name == "java.lang.Exception" || name == "java.lang.Throwable" {
			return true
		}
		if name == "SQLException" && gradedJavaLibraryType(u, name, "java.sql") || (name == "Exception" || name == "Throwable") && gradedJavaLibraryType(u, name, "java.lang") {
			return true
		}
		start = end + 1
	}
	return false
}
