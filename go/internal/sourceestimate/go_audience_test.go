package sourceestimate

import "testing"

func TestGoAudienceHelperAndUnrelatedSiblingInvariants(t *testing.T) {
	service := File{Path: "service.go", Language: "go", Source: []byte(`package sample
func Run(x int) int { return twice(x) }
func twice(x int) int { return x * 2 }
`)}
	inline := AnalyzeGoFiles([]File{service})[service.Path]
	moved := AnalyzeGoFiles([]File{
		{Path: "service.go", Language: "go", Source: []byte(`package sample
func Run(x int) int { return twice(x) }
`)},
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func twice(x int) int { return x * 2 }
`)},
	})
	withUnrelated := AnalyzeGoFiles([]File{
		{Path: "service.go", Language: "go", Source: []byte(`package sample
func Run(x int) int { return twice(x) }
`)},
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func twice(x int) int { return x * 2 }
`)},
		{Path: "unrelated.go", Language: "go", Source: []byte(`package sample
func Other(x, y, z int) int { return x }
`)},
	})["service.go"]
	if inline.Burden != moved["service.go"].Burden || inline.Hidden != moved["service.go"].Hidden {
		t.Fatalf("helper movement changed service projection: inline=%+v moved=%+v", inline, moved["service.go"])
	}
	if moved["service.go"].Burden != withUnrelated.Burden || moved["service.go"].Hidden != withUnrelated.Hidden {
		t.Fatalf("unrelated sibling changed service projection: moved=%+v sibling=%+v", moved["service.go"], withUnrelated)
	}
	for _, abstraction := range moved["service.go"].Abstractions {
		if abstraction.Name == "internal" {
			t.Fatalf("same-file helper became an independent abstraction: %+v", moved["service.go"].Abstractions)
		}
	}
}

func TestGoAudienceIndependentShallowRootRemainsVisible(t *testing.T) {
	result := AnalyzeGoFiles([]File{{Path: "service.go", Language: "go", Source: []byte(`package sample
func Run(x int) int { return x * 2 }
func local(x int) int { return x }
`)}})["service.go"]
	if len(result.Abstractions) < 2 {
		t.Fatalf("independent internal root was blended away: %+v", result)
	}
	foundShallow := false
	for _, abstraction := range result.Abstractions {
		if abstraction.Hidden == 0 && abstraction.Burden > 0 {
			foundShallow = true
		}
	}
	if !foundShallow {
		t.Fatalf("independent shallow root was masked by deeper work: %+v", result.Abstractions)
	}
}

func TestGoAudienceSiblingInternalRootUsesPackageAudience(t *testing.T) {
	results := AnalyzeGoFiles([]File{
		{Path: "service.go", Language: "go", Source: []byte(`package sample
func Run(x int) int { return helper(x) }
`)},
		{Path: "helper.go", Language: "go", Source: []byte(`package sample
func helper(x int) int { return x }
`)},
	})
	var found bool
	for _, abstraction := range results["helper.go"].Abstractions {
		if abstraction.Audience == "package" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sibling-called internal root lost package audience: %+v", results["helper.go"])
	}
}

func TestGoAudienceSupportingRepresentationsSurviveFileMovement(t *testing.T) {
	service := File{Path: "service.go", Language: "go", Source: []byte(`package sample
func Run(x int) int { return x * 2 }
`)}
	combined := []File{service, {Path: "support.go", Language: "go", Source: []byte(`package sample
type localError struct { message string }
func (e localError) Error() string { return e.message }
type data struct { value int }
func (d data) Value() int { return d.value }
`)}}
	split := []File{service,
		{Path: "error.go", Language: "go", Source: []byte(`package sample
type localError struct { message string }
func (e localError) Error() string { return e.message }
`)},
		{Path: "data.go", Language: "go", Source: []byte(`package sample
type data struct { value int }
func (d data) Value() int { return d.value }
`)},
	}
	combinedResults := AnalyzeGoFiles(combined)
	splitResults := AnalyzeGoFiles(split)
	if combinedResults["service.go"].Burden != splitResults["service.go"].Burden || combinedResults["service.go"].Hidden != splitResults["service.go"].Hidden {
		t.Fatalf("supporting representation movement changed service score: combined=%+v split=%+v", combinedResults["service.go"], splitResults["service.go"])
	}
	for _, path := range []string{"support.go", "error.go", "data.go"} {
		result := combinedResults[path]
		if path == "support.go" {
			result = combinedResults[path]
		} else {
			result = splitResults[path]
		}
		if !result.RoleOnly || result.Applicable || len(result.Supporting) == 0 {
			t.Fatalf("supporting representation lost role-only proof for %s: %+v", path, result)
		}
	}
}
