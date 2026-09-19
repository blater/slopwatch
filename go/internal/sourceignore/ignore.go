// Package sourceignore applies .gitignore rules independently of Git and its index.
package sourceignore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
)

type rule struct {
	base              string
	pattern           *regexp.Regexp
	negate, directory bool
}
type directoryRules struct {
	rules   []rule
	ignored bool
}

// Matcher caches parsed rules and directory decisions for one inventory scan.
// Paths remain logical: following a symlink does not change rule ownership.
type Matcher struct {
	mu             sync.Mutex
	root, boundary string
	disabled       bool
	directories    map[string]directoryRules
	inputs         map[string]ruleInput
}

func New(root string, disabled bool) *Matcher {
	root, _ = filepath.Abs(root)
	boundary := root
	for current := root; ; current = filepath.Dir(current) {
		boundary = current
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			break
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return &Matcher{root: root, boundary: boundary, disabled: disabled, directories: map[string]directoryRules{}, inputs: map[string]ruleInput{}}
}

func (m *Matcher) Ignored(path string, directory bool) bool {
	if m == nil || m.disabled {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.root, path)
	}
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if directory {
		return m.directory(path).ignored
	}
	parent := m.directory(filepath.Dir(path))
	return parent.ignored || matches(parent.rules, path, false)
}

func (m *Matcher) directory(path string) directoryRules {
	if cached, ok := m.directories[path]; ok {
		return cached
	}
	parent := directoryRules{}
	if path != m.boundary && filepath.Dir(path) != path {
		parent = m.directory(filepath.Dir(path))
	}
	result := directoryRules{rules: parent.rules, ignored: parent.ignored || matches(parent.rules, path, true)}
	if !result.ignored {
		inputPath := filepath.Join(path, ".gitignore")
		input := readInput(inputPath)
		m.inputs[inputPath] = input
		if input.kind == "file" {
			own := parse(path, input.data)
			if len(own) > 0 {
				result.rules = append(append([]rule(nil), parent.rules...), own...)
			}
		}
	}
	m.directories[path] = result
	return result
}

func matches(rules []rule, path string, directory bool) bool {
	ignored := false
	for _, r := range rules {
		if r.directory && !directory {
			continue
		}
		relative, err := filepath.Rel(r.base, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		if r.pattern.MatchString(byteRunes(filepath.ToSlash(relative))) {
			ignored = !r.negate
		}
	}
	return ignored
}

func parse(base, data string) []rule {
	var rules []rule
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSuffix(line, "\r")
		for strings.HasSuffix(line, " ") {
			slashes := 0
			for i := len(line) - 2; i >= 0 && line[i] == '\\'; i-- {
				slashes++
			}
			if slashes%2 == 1 {
				break
			}
			line = strings.TrimSuffix(line, " ")
		}
		if line == "" || line[0] == '#' {
			continue
		}
		r := rule{base: base}
		if line[0] == '!' {
			r.negate = true
			line = line[1:]
		}
		if line == "" {
			continue
		}
		r.directory = strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		anchored := strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		prefix := "(?s)^"
		if !anchored {
			prefix += "(?:.*/)?"
		}
		pattern, err := regexp.Compile(prefix + glob(line) + "$")
		if err == nil {
			r.pattern = pattern
			rules = append(rules, r)
		}
	}
	return rules
}

func glob(pattern string) string {
	var out strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '\\':
			if i+1 < len(pattern) {
				i++
				out.WriteString(regexp.QuoteMeta(byteRunes(pattern[i : i+1])))
			} else {
				return "["
			}
		case '*':
			fragment, end := globStar(pattern, i)
			out.WriteString(fragment)
			i = end
		case '?':
			out.WriteString("[^/]")
		case '[':
			end := globClassEnd(pattern, i)
			if end == len(pattern) {
				return "["
			}
			class, ok := globClass(pattern[i+1 : end])
			if !ok {
				return "["
			}
			out.WriteString(class)
			i = end

		default:
			out.WriteString(regexp.QuoteMeta(byteRunes(pattern[i : i+1])))
		}
	}
	return out.String()
}

func globStar(pattern string, start int) (string, int) {
	end := start
	for end+1 < len(pattern) && pattern[end+1] == '*' {
		end++
	}
	wholeSegment := (start == 0 || pattern[start-1] == '/') && (end+1 == len(pattern) || pattern[end+1] == '/')
	if end == start || !wholeSegment {
		return "[^/]*", end
	}
	if end+1 < len(pattern) {
		return "(?:.*/)?", end + 1
	}
	return ".*", end
}

func globClassEnd(pattern string, start int) int {
	end := start + 1
	if end < len(pattern) && (pattern[end] == '!' || pattern[end] == '^') {
		end++
	}
	if end < len(pattern) && pattern[end] == ']' {
		end++
	}
	for end < len(pattern) && pattern[end] != ']' {
		if pattern[end] == '[' && end+1 < len(pattern) && pattern[end+1] == ':' {
			if close := strings.Index(pattern[end+2:], ":]"); close >= 0 {
				end += close + 4
				continue
			}
		}
		if pattern[end] == '\\' && end+1 < len(pattern) {
			end++
		}
		end++
	}
	return end
}

// Character classes cannot consume a path separator, even when negated.
func globClass(body string) (string, bool) {
	if strings.HasPrefix(body, "!") {
		body = "^" + body[1:]
	}
	class, err := syntax.Parse("["+byteRunes(body)+"]", syntax.Perl)
	if err != nil {
		return "", false
	}
	if class.Op == syntax.OpLiteral && len(class.Rune) == 1 && class.Rune[0] == '/' {
		return "", false
	}
	if class.Op == syntax.OpCharClass {
		class.Rune = withoutSeparator(class.Rune)
		if len(class.Rune) == 0 {
			return "", false
		}
	}
	return class.String(), true
}

func withoutSeparator(ranges []rune) []rune {
	result := []rune{}
	for at := 0; at < len(ranges); at += 2 {
		lo, hi := ranges[at], ranges[at+1]
		if lo > '/' || hi < '/' {
			result = append(result, lo, hi)
			continue
		}
		if lo < '/' {
			result = append(result, lo, '/'-1)
		}
		if hi > '/' {
			result = append(result, '/'+1, hi)
		}
	}
	return result
}

// AncestorInputs includes missing files so creation of a new ancestor rule is
// observable, stopping at the nearest repository root (or filesystem root).
func (m *Matcher) AncestorInputs() []string {
	var result []string
	for current := m.root; ; current = filepath.Dir(current) {
		result = append(result, filepath.Join(current, ".gitignore"))
		if current == m.boundary || filepath.Dir(current) == current {
			return result
		}
	}
}

// ruleInput records both present and absent inputs without following symlinks.
// A changed read error also changes the snapshot, while a persistent error is
// reported as incidental evidence rather than silently presented as no rules.
type ruleInput struct{ kind, data, warning string }

func readInput(path string) ruleInput {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ruleInput{kind: "missing"}
	}
	if err != nil {
		return ruleInput{kind: "unreadable", warning: err.Error()}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ruleInput{kind: "symlink"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ruleInput{kind: "unreadable", warning: err.Error()}
	}
	return ruleInput{kind: "file", data: string(data)}
}

// Fingerprint hashes only rules captured by inventory/planning. It performs no
// directory traversal; missing rule files in visited directories also count.
func (m *Matcher) Fingerprint() string {
	if m == nil || m.disabled {
		return "gitignore-disabled-v2"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.directory(m.root)
	paths := make([]string, 0, len(m.inputs))
	for path := range m.inputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		input := m.inputs[path]
		for _, part := range []string{path, input.kind, input.data, input.warning} {
			h.Write([]byte(part))
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Unchanged revalidates the captured rule inputs before results are published.
func (m *Matcher) Unchanged() bool {
	if m == nil || m.disabled {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for path, expected := range m.inputs {
		if actual := readInput(path); actual != expected {
			return false
		}
	}
	return true
}

func (m *Matcher) Diagnostics() []map[string]any {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []map[string]any
	paths := make([]string, 0, len(m.inputs))
	for path := range m.inputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if warning := m.inputs[path].warning; warning != "" {
			result = append(result, map[string]any{"path": path, "severity": "info", "code": "sourceignore.unreadable", "message": "Cannot read ignore rules: " + warning, "attributes": map[string]any{"log_only": true, "classification": "incidental_ignore_read"}})
		}
	}
	return result
}

// Git wildmatch consumes filename bytes, including one byte for each ?.
// Latin-1 code points preserve that behavior in Go's rune-based regexp engine.
func byteRunes(value string) string {
	if strings.IndexFunc(value, func(r rune) bool { return r >= 128 }) < 0 {
		return value
	}
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		out.WriteRune(rune(value[i]))
	}
	return out.String()
}

// InputFingerprint reads one rule input, including missing/symlink/error state.
// Ancestor watchers use this for bounded polling on platforms where registering
// an ancestor directory would also open every unrelated child.
func InputFingerprint(path string) string {
	input := readInput(path)
	sum := sha256.Sum256([]byte(input.kind + "\x00" + input.data + "\x00" + input.warning))
	return hex.EncodeToString(sum[:])
}
