package native

import (
	"testing"

	"github.com/blater/slopwatch/internal/sourceestimate"
)

func TestRustSourceAudienceAndSupportAttribution(t *testing.T) {
	trait := sourceestimate.AnalyzeRustWithAttribution([]sourceestimate.File{{Path: "service.rs", Language: "rust", Source: []byte(`pub trait Service { fn run(&self, x: i32) -> i32; }
struct Private;
impl Service for Private { fn run(&self, x: i32) -> i32 { x * 2 } }
`)}})
	traitResult := trait.Results["service.rs"]
	if !traitResult.Applicable || traitResult.Hidden != 2 || len(trait.RouteFamilies["service.rs"]) != 1 || trait.RouteFamilies["service.rs"][0].Audience != "external" {
		t.Fatalf("public trait/private representation attribution = %+v", traitResult)
	}
	valueSupport := sourceestimate.AnalyzeRustWithAttribution([]sourceestimate.File{{Path: "value.rs", Language: "rust", Source: []byte(`pub trait Value { fn value(&self) -> i32; }
struct Private { value: i32 }
impl Value for Private { fn value(&self) -> i32 { self.value } }
pub fn service(x: i32) -> i32 { x * 2 }
`)}})
	valueResult := valueSupport.Results["value.rs"]
	if !valueResult.Applicable || valueResult.Hidden != 2 || len(valueResult.Roles) == 0 {
		t.Fatalf("passive trait representation crowded service root: %+v", valueResult)
	}

	internal := sourceestimate.AnalyzeRustWithAttribution([]sourceestimate.File{{Path: "module.rs", Language: "rust", Source: []byte(`mod internal { fn helper(x: i32) -> i32 { x * 2 } pub fn run(x: i32) -> i32 { helper(x) } }
`)}})
	internalResult := internal.Results["module.rs"]
	if !internalResult.Applicable || len(internal.RouteFamilies["module.rs"]) != 1 || internal.RouteFamilies["module.rs"][0].Audience != "internal" {
		t.Fatalf("private module helper/root attribution = %+v", internalResult)
	}

	service := sourceestimate.File{Path: "service.rs", Language: "rust", Source: []byte(`pub fn run(x: i32) -> i32 { x * 2 }
`)}
	withSupport := sourceestimate.AnalyzeRustWithAttribution([]sourceestimate.File{
		{Path: "service.rs", Language: "rust", Source: []byte(`struct Supporting { value: i32 }
pub fn run(x: i32) -> i32 { x * 2 }
`)},
	})
	movedSupport := sourceestimate.AnalyzeRustWithAttribution([]sourceestimate.File{
		service,
		{Path: "support.rs", Language: "rust", Source: []byte(`struct Supporting { value: i32 }
`)},
	})
	if withSupport.Results["service.rs"].Hidden != movedSupport.Results["service.rs"].Hidden || withSupport.Results["service.rs"].Burden != movedSupport.Results["service.rs"].Burden {
		t.Fatalf("supporting type movement changed service score: alongside=%+v moved=%+v", withSupport.Results["service.rs"], movedSupport.Results["service.rs"])
	}

	direct := sourceestimate.AnalyzeRustWithAttribution([]sourceestimate.File{{Path: "run.rs", Language: "rust", Source: []byte(`pub fn run(x: i32) -> i32 { x * 2 }
`)}}).Results["run.rs"]
	extracted := sourceestimate.AnalyzeRustWithAttribution([]sourceestimate.File{{Path: "run.rs", Language: "rust", Source: []byte(`pub fn run(x: i32) -> i32 { helper(x) }
fn helper(x: i32) -> i32 { x * 2 }
`)}}).Results["run.rs"]
	if direct.Burden != extracted.Burden || direct.Hidden != extracted.Hidden {
		t.Fatalf("private helper extraction changed Rust score: direct=%+v extracted=%+v", direct, extracted)
	}

	identity := sourceestimate.AnalyzeRustWithAttribution([]sourceestimate.File{{Path: "main.rs", Language: "rust", Source: []byte(`pub fn main_value(x: i32) -> i32 { x }
`)}}).Results["main.rs"]
	if identity.Hidden != 0 || identity.Burden == 0 {
		t.Fatalf("independent shallow Rust root was lost: %+v", identity)
	}
}
