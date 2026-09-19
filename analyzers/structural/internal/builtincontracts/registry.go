// Package builtincontracts admits only exact, provenance-checked library contracts.
// Admission does not imply that an adapter implements a contract's flow lowering.
package builtincontracts

import (
	_ "embed"
	"encoding/json"
)

//go:embed registry.json
var manifest []byte

type Match struct {
	Symbol     string   `json:"symbol"`
	Signature  string   `json:"signature"`
	Parameters []string `json:"parameters"`
	Provenance string   `json:"provenance"`
	Guards     []string `json:"guards"`
}
type Normal struct {
	Result  string   `json:"result"`
	Alias   string   `json:"alias"`
	Effects []string `json:"effects"`
}
type Entry struct {
	ID           string   `json:"id"`
	Language     string   `json:"language"`
	Match        Match    `json:"match"`
	Normal       Normal   `json:"normal"`
	Failures     []string `json:"ordinary_failures"`
	Policies     []string `json:"policy_positions"`
	HiddenCredit string   `json:"hidden_credit"`
}
type registry struct {
	Version string  `json:"version"`
	Entries []Entry `json:"entries"`
}

var approved = readRegistry()

func readRegistry() registry {
	var out registry
	if err := json.Unmarshal(manifest, &out); err != nil {
		panic(err)
	}
	return out
}
func Version() string { return approved.Version }

func Lookup(language, symbol, signature, provenance string, verifiedGuards []string) (Entry, bool) {
	for _, entry := range approved.Entries {
		if entry.Language != language || entry.Match.Symbol != symbol || entry.Match.Signature != signature || entry.Match.Provenance != provenance {
			continue
		}
		if !guardsVerified(entry.Match.Guards, verifiedGuards) {
			return Entry{}, false
		}
		return copyEntry(entry), true
	}
	return Entry{}, false
}
func guardsVerified(required, verified []string) bool {
	set := map[string]bool{}
	for _, guard := range verified {
		set[guard] = true
	}
	for _, guard := range required {
		if !set[guard] {
			return false
		}
	}
	return true
}
func copyEntry(entry Entry) Entry {
	entry.Match.Parameters = append([]string(nil), entry.Match.Parameters...)
	entry.Match.Guards = append([]string(nil), entry.Match.Guards...)
	entry.Normal.Effects = append([]string(nil), entry.Normal.Effects...)
	entry.Failures = append([]string(nil), entry.Failures...)
	entry.Policies = append([]string(nil), entry.Policies...)
	return entry
}
