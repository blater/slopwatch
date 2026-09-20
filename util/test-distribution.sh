#!/usr/bin/env bash
# Shared packaged-distribution tests for make test, CI, and releases.
set -euo pipefail
cd "$(dirname "$0")/.."
test_tmp=$(mktemp -d)
trap 'rm -rf "$test_tmp"' EXIT
VERSION=${VERSION:-dev}

stage="$test_tmp/slopwatch-stage"
mkdir -p \
  "$stage/build" \
  "$stage/analyzers/structural" \
  dist
cp build/slopmark build/slopwatch "$stage/build/"
cp -R build/typescript "$stage/build/"
cp \
  analyzers/structural/slopslap-structural \
  analyzers/structural/slopslap-structural-rust \
  analyzers/structural/slopslap-structural-java.jar \
  "$stage/analyzers/structural/"
cp -R analyzers/structural/java-runtime "$stage/analyzers/structural/"
cp component-catalog.json LICENSE "$stage/"
archive="dist/slopwatch-${VERSION}-darwin-arm64.tar.gz"
tar -C "$stage" -czf "$archive" .
shasum -a 256 "$archive" > dist/SHA256SUMS

unpack="$test_tmp/slopwatch-unpack"
source="$test_tmp/slopwatch-smoke"
mkdir -p "$unpack" "$source"
tar -xzf "dist/slopwatch-${VERSION}-darwin-arm64.tar.gz" -C "$unpack"
cat > "$source/main.go" <<'GO'
package smoke
import "fmt"
type Peer struct { A, B, C, D, E, F int }
type Service struct { state int }
func (s *Service) GoRoutine(peer Peer) int {
  _ = fmt.Sprint(peer.A)
  if peer.A > 0 {}; if peer.A > 1 {}; if peer.A > 2 {}; if peer.A > 3 {}
  if peer.B > 0 {}; if peer.B > 1 {}; if peer.B > 2 {}; if peer.B > 3 {}
  if peer.C > 0 {}; if peer.C > 1 {}; if peer.C > 2 {}; if peer.C > 3 {}
  if peer.D > 0 {}; if peer.D > 1 {}; if peer.D > 2 {}; if peer.D > 3 {}
  if peer.E > 0 {}; if peer.E > 1 {}; if peer.E > 2 {}; if peer.E > 3 {}
  if peer.F > 0 {}; if peer.F > 1 {}; if peer.F > 2 {}; if peer.F > 3 {}
  if peer.A > 4 {}; if peer.B > 4 {}; if peer.C > 4 {}; if peer.D > 4 {}
  if peer.E > 4 {}; if peer.F > 4 {}; if peer.A > 5 {}; if peer.B > 5 {}
  if peer.C > 5 {}; if peer.D > 5 {}; if peer.E > 5 {}; if peer.F > 5 {}
  if peer.A > 6 {}; if peer.B > 6 {}; if peer.C > 6 {}; if peer.D > 6 {}
  if peer.E > 6 {}; if peer.F > 6 {}; if peer.A > 7 {}; if peer.B > 7 {}
  if peer.C > 7 {}; if peer.D > 7 {}; if peer.E > 7 {}; if peer.F > 7 {}
  return s.state + peer.A + peer.B + peer.C + peer.D + peer.E + peer.F
}
GO
cat > "$source/Main.java" <<'JAVA'
class Main { void javaRoutine(boolean ok) { if (ok) {} } }
JAVA
cat > "$source/main.rs" <<'RUST'
fn rust_routine(ok: bool) { if ok {} }
RUST
cat > "$source/main.ts" <<'TYPESCRIPT'
export function typeScriptRoutine(ok: boolean): void { if (ok) {} }
TYPESCRIPT
cat > "$source/tsconfig.json" <<'JSON'
{"compilerOptions":{"strict":true},"include":["main.ts"]}
JSON
(
  cd "$source"
  env -u GOROOT "$unpack/build/slopmark" --format json --score-profile legacy-signature-v3 . > "$test_tmp/report.json"
)
for language in go java rust typescript; do
  grep -Eq "\"language\"[[:space:]]*:[[:space:]]*\"$language\"" "$test_tmp/report.json" || {
    echo "Packaged smoke test omitted $language"
    exit 1
  }
done
jq -e '
  .files[]
  | select(.language == "go")
  | (.coverage.coupling_between_objects == "complete")
    and (.coverage.god_class == "complete")
    and (([.components.coupling_between_objects.evidence[].value] | max) > 0)
    and (.components.god_class.contribution > 0)
' "$test_tmp/report.json" >/dev/null || {
  echo "Packaged Go analyzer did not produce complete, non-zero CPL and GOD metrics for imported code"
  jq '.files[] | select(.language == "go") | {coverage, coupling: .components.coupling_between_objects, god: .components.god_class}' "$test_tmp/report.json"
  exit 1
}
mixed="$test_tmp/slopwatch-smoke-mixed"
cp -R "$source" "$mixed"
mkdir -p "$mixed/broken"
cat > "$mixed/broken/broken.go" <<'GO'
package broken
func Broken( {
GO
cat > "$mixed/broken/Broken.java" <<'JAVA'
class Broken { void broken( { }
JAVA
cat > "$mixed/broken/broken.rs" <<'RUST'
fn broken( {
RUST
cat > "$mixed/broken/broken.ts" <<'TYPESCRIPT'
export function broken(ok: boolean): void { if (ok) {
TYPESCRIPT
(
  cd "$mixed"
  env -u GOROOT "$unpack/build/slopmark" --format json --score-profile legacy-signature-v3 . > "$test_tmp/mixed-report.json"
)
jq -e '
  . as $report
  | [{path: "broken/broken.go", language: "go", code: "SYNTAX_ERROR"},
     {path: "broken/Broken.java", language: "java", code: "SYNTAX_ERROR"},
     {path: "broken/broken.rs", language: "rust", code: "SYNTAX_ERROR"},
     {path: "broken/broken.ts", language: "typescript", code: "typescript.syntax."}] as $broken
  | all($broken[];
      . as $expected
      | any($report.files[];
          .path == $expected.path
          and .language == $expected.language
          and (.complete == false)
          and ([(.coverage // {})[]] | length > 0 and all(.[]; . == "failed")))
      and any(($report.diagnostics // [])[];
          .path == $expected.path
          and .severity == "error"
          and ((.message // "") | length > 0)
          and (if ($expected.code | endswith("."))
               then (.code | startswith($expected.code))
               else .code == $expected.code
               end)))
  and (["main.go", "main.ts"]
      | all(.[];
          . as $path
          | any($report.files[]; .path == $path and .complete == true)))
  and any($report.files[];
      .path == "main.go"
      and .language == "go"
      and .complete == true
      and (.components.god_class.contribution > 0))
  and any($report.files[];
      .path == "Main.java"
      and .language == "java"
      and .complete == false
      and (.coverage.cyclomatic_method_complexity == "complete")
      and (.coverage.cognitive_complexity == "complete")
      and (.coverage.npath_complexity == "complete")
      and (.coverage.coupling_between_objects == "unavailable")
      and (.coverage.cyclomatic_class_complexity == "unavailable")
      and (.coverage.god_class == "unavailable")
      and any(.components.cyclomatic_method_complexity.evidence[];
          .routine == "Main.javaRoutine" and .value > 0))
  and any($report.files[];
      .path == "main.rs"
      and .language == "rust"
      and (.coverage.cyclomatic_method_complexity == "complete")
      and (.coverage.cognitive_complexity == "complete")
      and (.coverage.npath_complexity == "complete")
      and (.coverage.coupling_between_objects == "unavailable")
      and any(.components.cyclomatic_method_complexity.evidence[];
          .routine == "rust_routine" and .value > 0))
' "$test_tmp/mixed-report.json" >/dev/null || {
  echo "Packaged syntax recovery smoke test did not retain healthy files or failed broken-file coverage"
  jq '{files: [.files[] | {path, language, complete, coverage}], diagnostics}' "$test_tmp/mixed-report.json"
  exit 1
}
"$unpack/build/slopmark" --help >/dev/null
"$unpack/build/slopwatch" --help >/dev/null

source="$test_tmp/slopwatch-smoke-v4"
mkdir -p "$source/rust/src" "$source/typescript"
cat > "$source/go.mod" <<'MOD'
module smoke

go 1.24
MOD
cat > "$source/service.go" <<'GO'
package smoke
func Run(x int) int { return x + 1 }
GO
mkdir -p "$source/java"
cat > "$source/java/Service.java" <<'JAVA'
package smoke;
public final class Service {
  private Service() {}
  public static int run(int x) { return x + 1; }
}
JAVA
cat > "$source/rust/Cargo.toml" <<'TOML'
[package]
name = "smoke"
version = "0.1.0"
edition = "2021"
TOML
cat > "$source/rust/src/lib.rs" <<'RUST'
pub fn run(x: i32) -> i32 { x.wrapping_add(1) }
RUST
cat > "$source/typescript/service.ts" <<'TYPESCRIPT'
export function run(x: number): number { return x + 1; }
TYPESCRIPT
cat > "$source/typescript/tsconfig.json" <<'JSON'
{"compilerOptions":{"strict":true},"include":["service.ts"]}
JSON
(
  cd "$source"
  env -u GOROOT "$unpack/build/slopmark" --format json . > "$test_tmp/v4-report.json"
  env -u GOROOT "$unpack/build/slopmark" --format json --score-profile legacy-signature-v3 . > "$test_tmp/v4-legacy-report.json"
)
# Published grading and the descriptive ratio are separate. Rust retains its
# unresolved-call limitation; non-SHALLOW metrics must remain profile-independent.
jq -e '
  . as $report
  | $report.schema_version == 4
    and $report.score_profile == "responsibility-v4"
    and all(["go", "java", "rust", "typescript"][];
      . as $language
      | any($report.files[];
          .language == $language
          and .complete == (.language != "rust")
          and .components.module_shallowness.depth_version == "responsibility-burden-v4"
          and (if .language == "rust"
               then .components.module_shallowness.depth_state == "partial"
                 and .components.module_shallowness.depth_estimated == true
               else .components.module_shallowness.depth_state == "measured"
                 and (.components.module_shallowness.depth_estimated != true)
               end)
          and .components.module_shallowness.raw_max == 0
          and .components.module_shallowness.contribution == 0))
    and ($report.depth | length == 4)
    and all($report.depth[];
      .shallow == 0
      and .raw.responsibility_ratio == 30
      and .raw.penalty_basis == "graded-caller-responsibility"
      and .raw.graded.zero_reason == "lowest_range_supported"
      and (if (.files | index("rust/src/lib.rs")) != null
           then .estimated == true
             and (.raw.graded.material_limitations
                  | index("unresolved_call_range_0_2:x.wrapping_add/1")) != null
           else .raw.graded.hidden_responsibility == 2
             and (.raw.graded.material_limitations | length) == 0
           end))
' "$test_tmp/v4-report.json" >/dev/null || {
  echo "Packaged default v4 smoke test did not preserve numeric ratings and evidence status"
  jq '{schema_version, score_profile, policy_revision, files: [.files[] | {path, language, complete, shallow: .components.module_shallowness}]}' "$test_tmp/v4-report.json"
  exit 1
}
jq -S '
  {files: [.files[] | {
    path, language,
    coverage: ((.coverage // {}) | del(.module_shallowness)),
    components: ((.components // {}) | del(.module_shallowness))
  }]}
' "$test_tmp/v4-report.json" > "$test_tmp/v4-nonshallow.json"
jq -S '
  {files: [.files[] | {
    path, language,
    coverage: ((.coverage // {}) | del(.module_shallowness)),
    components: ((.components // {}) | del(.module_shallowness))
  }]}
' "$test_tmp/v4-legacy-report.json" > "$test_tmp/v4-legacy-nonshallow.json"
diff -u "$test_tmp/v4-legacy-nonshallow.json" "$test_tmp/v4-nonshallow.json"

echo "Packaged distribution smoke tests passed ($archive)"
