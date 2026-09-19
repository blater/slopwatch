package javaadapter

import (
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestJavaDepthAttributionRespectsSuppliedSourceInventory(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	service := "src/main/java/demo/Service.java"
	helper := "src/main/java/demo/Helper.java"
	writeSource(t, root, service, `
package demo;
import demo.Helper;
public final class Service {
  private Service() {}
  public static int run(int value) { return Helper.identity(value); }
}`)
	writeSource(t, root, helper, `
package demo;
public final class Helper {
  private Helper() {}
  public static int identity(int value) { return value; }
}`)

	omitted, err := adapter.Analyze(root, []string{service}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	omittedService := javaDepthBoundaryBySymbol(t, omitted, "demo.Service")
	if omittedService.State != facts.KnowledgePartial || !hasJavaDepthReasonPrefix(omittedService, "attribution_failed") {
		t.Fatalf("omitted helper was attributed: %#v", omittedService)
	}

	supplied, err := adapter.Analyze(root, []string{service, helper}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	suppliedService := javaDepthBoundaryBySymbol(t, supplied, "demo.Service")
	if suppliedService.State != facts.KnowledgeMeasured || hasJavaDepthReasonPrefix(suppliedService, "attribution_failed") {
		t.Fatalf("supplied helper did not resolve cleanly: %#v", suppliedService)
	}

	broken := "src/main/java/demo/Broken.java"
	writeSource(t, root, broken, "package demo; public class Broken { public static int run( { return 1; }\n")
	recovered, err := adapter.Analyze(root, []string{service, broken}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatalf("syntax-broken sibling terminated attribution: %v", err)
	}
	recoveredService := javaDepthBoundaryBySymbol(t, recovered, "demo.Service")
	if recoveredService.State != facts.KnowledgePartial || !hasJavaDepthReasonPrefix(recoveredService, "incomplete_source_inventory") {
		t.Fatalf("good boundary was not retained as partial: %#v", recoveredService)
	}
}

func javaDepthBoundaryBySymbol(t *testing.T, program *facts.Program, symbol string) *facts.BoundaryAssessment {
	t.Helper()
	if program.Depth == nil {
		t.Fatal("missing Java depth facts")
	}
	for index := range program.Depth.Boundaries {
		boundary := &program.Depth.Boundaries[index]
		if boundary.Identity.Symbol == symbol {
			return boundary
		}
	}
	t.Fatalf("missing Java depth boundary %q: %#v", symbol, program.Depth.Boundaries)
	return nil
}

func hasJavaDepthReasonPrefix(boundary *facts.BoundaryAssessment, prefix string) bool {
	for _, reason := range boundary.Reasons {
		if strings.HasPrefix(reason.Code, prefix) {
			return true
		}
	}
	return false
}
