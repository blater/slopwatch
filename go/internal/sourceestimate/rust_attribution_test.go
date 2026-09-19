package sourceestimate

import (
	"strings"
	"testing"
)

func rustResult(source string) Result {
	results, _ := AnalyzeRustAttribution([]File{{Path: "src/lib.rs", Language: "rust", Source: []byte(source)}})
	return results["src/lib.rs"]
}

func TestRustAttributionPublicTraitOnPrivateRepresentationKeepsServiceRoot(t *testing.T) {
	source := `pub trait Service { fn run(&self, x: i32) -> i32; }
struct Private;
impl Service for Private { fn run(&self, x: i32) -> i32 { helper(x) } }
fn helper(x: i32) -> i32 { x * 2 }
`
	result := rustResult(source)
	if !result.Applicable || result.Burden != 1.25 || result.Hidden != 2 {
		t.Fatalf("public trait implementation attribution = %+v", result)
	}
	if len(result.Abstractions) != 1 || result.Abstractions[0].Audience != "external" {
		t.Fatalf("trait implementation roots = %+v", result.Abstractions)
	}
}

func TestRustAttributionDropCleanupIsNotAnIndependentRoot(t *testing.T) {
	result := rustResult(`struct Release<'a>(&'a mut Resource);
impl Drop for Release<'_> { fn drop(&mut self) { self.0.close() } }
struct Resource { open: bool }
impl Resource { fn close(&mut self) { self.open = false } }
pub struct Example { resource: Resource }
impl Example { pub fn run(&mut self) { let guard = Release(&mut self.resource); drop(guard); } }
`)
	if !result.Applicable || len(result.Abstractions) != 1 {
		t.Fatalf("Drop cleanup became an independent root: %+v", result.Abstractions)
	}
	if result.Abstractions[0].Audience != "external" || !strings.Contains(result.Abstractions[0].Name, "Example") {
		t.Fatalf("unexpected Rust lifecycle route: %+v", result.Abstractions)
	}
}

func TestRustAttributionReceiverIsNotCallerInput(t *testing.T) {
	result := rustResult(`pub struct Service;
impl Service { pub fn run(&self, value: i32) -> i32 { value } }
`)
	if !result.Applicable || result.Burden != 1.25 {
		t.Fatalf("Rust receiver counted as caller input: %+v", result)
	}
}

func TestRustAttributionPrivateHelperExtractionIsStable(t *testing.T) {
	direct := rustResult(`pub struct Service;
impl Service { pub fn run(&self, x: i32) -> i32 { x * 2 } }
`)
	delegated := rustResult(`pub struct Service;
impl Service { pub fn run(&self, x: i32) -> i32 { helper(x) } }
fn helper(x: i32) -> i32 { x * 2 }
`)
	if !direct.Applicable || !delegated.Applicable || direct.Burden != delegated.Burden || direct.Hidden != delegated.Hidden {
		t.Fatalf("private helper extraction changed Rust estimate: direct=%+v delegated=%+v", direct, delegated)
	}
}

func TestRustAttributionSupportingTypesAndIndependentModuleWork(t *testing.T) {
	service := rustResult(`pub struct Service;
impl Service { pub fn run(&self, x: i32) -> i32 { x * 2 } }
`)
	withSupportingType := rustResult(`struct Supporting { value: i32 }
pub struct Service;
impl Service { pub fn run(&self, x: i32) -> i32 { x * 2 } }
`)
	if service.Burden != withSupportingType.Burden || service.Hidden != withSupportingType.Hidden {
		t.Fatalf("supporting type changed service projection: service=%+v with=%+v", service, withSupportingType)
	}
	value := rustResult(`pub struct Point { pub x: i32, pub y: i32 }
`)
	if value.Applicable || !value.RoleOnly || value.Burden != 0 || value.Hidden != 0 || len(value.Roles) != 1 || value.Roles[0] != roleSupportingRustType {
		t.Fatalf("supporting Rust type attribution = %+v", value)
	}
	independent := rustResult(`fn local(x: i32) -> i32 { x }
`)
	if !independent.Applicable || independent.Burden != 1.25 || independent.Hidden != 0 {
		t.Fatalf("independent module function disappeared: %+v", independent)
	}
}

func TestRustAttributionUnrelatedSiblingDoesNotChangeService(t *testing.T) {
	service := File{Path: "service.rs", Language: "rust", Source: []byte(`pub fn run(x: i32) -> i32 { x * 2 }
`)}
	without := AnalyzeRustWithAttribution([]File{service}).Results[service.Path]
	with := AnalyzeRustWithAttribution([]File{service, File{Path: "sibling.rs", Language: "rust", Source: []byte(`pub fn other(x: i32) -> i32 { x + 1 }
`)}}).Results[service.Path]
	if without.Burden != with.Burden || without.Hidden != with.Hidden {
		t.Fatalf("unrelated Rust sibling changed service projection: without=%+v with=%+v", without, with)
	}
}

func TestRustAttributionPrivateModuleDoesNotBecomeExternal(t *testing.T) {
	source := `mod internal { pub fn run(x: i32) -> i32 { x * 2 } }
`
	result := rustResult(source)
	if !result.Applicable || len(result.Abstractions) != 1 || result.Abstractions[0].Audience != "internal" {
		t.Fatalf("private-module attribution = %+v", result)
	}
}
