package javaadapter

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

func TestJavaSupportingContractInternalInjectedProviderIsNotApplicable(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	paths := []string{"src/main/java/sample/Allocator.java", "src/main/java/sample/Provider.java", "src/main/java/sample/Arena.java", "src/main/java/sample/Wiring.java"}
	writeSource(t, root, paths[0], `package sample;
interface Allocator { byte[] allocate(int bytes); }`)
	writeSource(t, root, paths[1], `package sample;
final class Provider implements Allocator {
  static final Provider INSTANCE = new Provider();
  private Provider() {}
  public byte[] allocate(int bytes) { return new byte[bytes]; }
}`)
	writeSource(t, root, paths[2], `package sample;
final class Arena {
  private final Allocator allocator;
  Arena() { this( Wiring.HEAP ); }
  Arena(Allocator value) { allocator = value; }
  byte[] reserve(int bytes) { return allocator.allocate(bytes); }
}`)
	writeSource(t, root, paths[3], `package sample;
final class Wiring { static final Allocator HEAP = Provider.INSTANCE; }`)

	program, err := adapter.Analyze(root, paths, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "sample.Provider")
	if len(boundary.RouteFamilies) == 0 {
		t.Fatal("supporting implementation signatures disappeared from inventory")
	}
	role := boundary.SupportingContract
	if role == nil || role.Inventory != facts.KnowledgeMeasured || role.Exposure != facts.KnowledgeMeasured {
		t.Fatalf("supporting role = %#v", role)
	}
	if !slices.Equal(role.Members, []string{"sample.Provider#allocate(int)"}) || len(role.ExternalRoutes) != 0 {
		t.Fatalf("supporting role inventory = %#v", role)
	}
	if len(role.Bindings) != 1 || role.Bindings[0].ContractMember != "sample.Allocator#allocate(int)" || role.Bindings[0].Slot == "" || role.Bindings[0].Use == "" {
		t.Fatalf("supporting role binding = %#v", role.Bindings)
	}
	scores := metrics.MeasureDepth(program)
	score := scoreForJavaBoundary(scores, "sample.Provider")
	if score == nil || score.State != facts.KnowledgeNotApplicable || score.Shallow != nil {
		t.Fatalf("supporting contract score = %#v", scores)
	}
}

func TestJavaSupportingContractRejectsPublicExposureAndUnmatchedMember(t *testing.T) {
	tests := []struct {
		name, extra string
		wantRoute   bool
	}{
		{"public factory", ``, true},
		{"unmatched public member", `public byte[] copy(int bytes) { return new byte[bytes]; }`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, adapter := javaTestAdapter(t)
			provider := `package sample; final class Provider implements Allocator {
  static final Provider INSTANCE = new Provider(); private Provider() {}
  public byte[] allocate(int bytes) { return new byte[bytes]; }
  ` + test.extra + `
}`
			paths := []string{"src/main/java/sample/Allocator.java", "src/main/java/sample/Provider.java", "src/main/java/sample/Arena.java", "src/main/java/sample/Wiring.java"}
			writeSource(t, root, paths[0], `package sample; interface Allocator { byte[] allocate(int bytes); }`)
			writeSource(t, root, paths[1], provider)
			writeSource(t, root, paths[2], `package sample; final class Arena { private final Allocator allocator;
  Arena() { this(Wiring.HEAP); } Arena(Allocator value) { allocator = value; }
  byte[] reserve(int bytes) { return allocator.allocate(bytes); } }`)
			writeSource(t, root, paths[3], `package sample; final class Wiring { static final Allocator HEAP = Provider.INSTANCE; }`)
			if test.wantRoute {
				writeSource(t, root, "src/main/java/sample/Factory.java", `package sample; public final class Factory {
				  public static Object make() { Object first = hidden(); Object second = first; return (second); }
				  private static Object hidden() { return new Provider(); }
}`)
				paths = append(paths, "src/main/java/sample/Factory.java")
			}
			program, err := adapter.Analyze(root, paths, map[string]any{"depth_profile": "responsibility-v4"})
			if err != nil {
				t.Fatal(err)
			}
			boundary := javaDepthBoundaryBySymbol(t, program, "sample.Provider")
			if boundary.SupportingContract == nil {
				t.Fatalf("missing role: %#v", boundary)
			}
			if test.wantRoute && len(boundary.SupportingContract.ExternalRoutes) == 0 {
				t.Fatalf("public factory was not exposed: %#v", boundary.SupportingContract)
			}
			if !test.wantRoute && !slices.Contains(boundary.SupportingContract.Members, "sample.Provider#copy(int)") {
				t.Fatalf("unmatched member was not inventoried: %#v", boundary.SupportingContract)
			}
			scores := metrics.MeasureDepth(program)
			score := scoreForJavaBoundary(scores, "sample.Provider")
			if score == nil || score.State == facts.KnowledgeNotApplicable {
				t.Fatalf("negative supporting contract score = %#v", scores)
			}
			if test.wantRoute && !strings.Contains(strings.Join(boundary.SupportingContract.ExternalRoutes, "\n"), "Factory") {
				t.Fatalf("factory route evidence = %#v", boundary.SupportingContract.ExternalRoutes)
			}
		})
	}
}

func TestJavaSupportingContractExposureCutoffRemainsPartial(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	paths := []string{"src/main/java/cap/Allocator.java", "src/main/java/cap/Provider.java", "src/main/java/cap/Arena.java", "src/main/java/cap/Wiring.java", "src/main/java/cap/Factory.java"}
	writeSource(t, root, paths[0], `package cap; interface Allocator { byte[] allocate(int bytes); }`)
	writeSource(t, root, paths[1], `package cap; final class Provider implements Allocator {
  static final Provider INSTANCE = new Provider(); private Provider() {}
  public byte[] allocate(int bytes) { return new byte[bytes]; }
}`)
	writeSource(t, root, paths[2], `package cap; final class Arena { private final Allocator allocator;
  Arena() { this(Wiring.HEAP); } Arena(Allocator value) { allocator = value; }
  byte[] reserve(int bytes) { return allocator.allocate(bytes); } }`)
	writeSource(t, root, paths[3], `package cap; final class Wiring { static final Allocator HEAP = Provider.INSTANCE; }`)
	for index := 0; index < 260; index++ {
		path := "src/main/java/cap/Link" + strconv.Itoa(index) + ".java"
		paths = append(paths, path)
		body := "return Provider.INSTANCE;"
		if index < 259 {
			body = "return Link" + strconv.Itoa(index+1) + ".make();"
		}
		writeSource(t, root, path, "package cap; final class Link"+strconv.Itoa(index)+" { static Object make() { "+body+" } }")
	}
	writeSource(t, root, paths[4], `package cap; public final class Factory { public static Object make() { return Link0.make(); } }`)
	program, err := adapter.Analyze(root, paths, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	role := javaDepthBoundaryBySymbol(t, program, "cap.Provider").SupportingContract
	if role == nil || role.Exposure != facts.KnowledgePartial {
		t.Fatalf("cutoff exposure = %#v", role)
	}
}

func TestJavaSupportingContractAttributionErrorsArePackageScoped(t *testing.T) {
	for _, test := range []struct {
		name, brokenPath, brokenPackage string
		wantInventory                   facts.KnowledgeState
	}{
		{"unrelated package", "src/main/java/other/Broken.java", "other", facts.KnowledgeMeasured},
		{"same package", "src/main/java/sample/Broken.java", "sample", facts.KnowledgePartial},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, adapter := javaTestAdapter(t)
			paths := []string{"src/main/java/sample/Allocator.java", "src/main/java/sample/Provider.java", "src/main/java/sample/Arena.java", "src/main/java/sample/Wiring.java", test.brokenPath}
			writeSource(t, root, paths[0], `package sample; interface Allocator { byte[] allocate(int bytes); }`)
			writeSource(t, root, paths[1], `package sample; final class Provider implements Allocator {
  static final Provider INSTANCE = new Provider(); private Provider() {}
  public byte[] allocate(int bytes) { return new byte[bytes]; }
}`)
			writeSource(t, root, paths[2], `package sample; final class Arena { private final Allocator allocator;
  Arena() { this(Wiring.HEAP); } Arena(Allocator value) { allocator = value; }
  byte[] reserve(int bytes) { return allocator.allocate(bytes); } }`)
			writeSource(t, root, paths[3], `package sample; final class Wiring { static final Allocator HEAP = Provider.INSTANCE; }`)
			writeSource(t, root, test.brokenPath, "package "+test.brokenPackage+`; final class Broken { Missing value; }`)
			program, err := adapter.Analyze(root, paths, map[string]any{"depth_profile": "responsibility-v4"})
			if err != nil {
				t.Fatal(err)
			}
			boundary := javaDepthBoundaryBySymbol(t, program, "sample.Provider")
			role := boundary.SupportingContract
			if role == nil || role.Inventory != test.wantInventory || role.Exposure != test.wantInventory {
				t.Fatalf("scoped role = %#v", role)
			}
			if test.wantInventory == facts.KnowledgePartial && boundary.State != facts.KnowledgePartial {
				t.Fatalf("partial scoped boundary state = %#v", boundary)
			}
			score := scoreForJavaBoundary(metrics.MeasureDepth(program), "sample.Provider")
			if score == nil {
				t.Fatalf("missing scoped role score")
			}
			if test.wantInventory == facts.KnowledgePartial &&
				(score.State == facts.KnowledgeNotApplicable || score.Shallow != nil && !score.Estimated) {
				t.Fatalf("partial scoped role score = %#v", score)
			}
			if test.wantInventory == facts.KnowledgePartial && score.Estimated {
				retained := false
				for _, reason := range score.PreciseReasons {
					retained = retained || reason.Code == "incomplete_supporting_role"
				}
				if !retained {
					t.Fatal("estimated score discarded incomplete supporting-role evidence")
				}
			}
		})
	}
}

func TestJavaDepthAdmitsStatelessInstanceSurface(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	path := "src/main/java/sample/Scalar.java"
	writeSource(t, root, path, `package sample;
public final class Scalar {
  public static final int LIMIT = 8;
  public Scalar() {}
  public int add(int value) { if (value > 0) return value + 1; return 0; }
}`)
	program, err := adapter.Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "sample.Scalar")
	if boundary.State != facts.KnowledgeMeasured || hasJavaDepthReasonPrefix(boundary, "unsupported_") {
		t.Fatalf("stateless instance boundary = %#v", boundary)
	}
	scores := metrics.MeasureDepth(program)
	score := scoreForJavaBoundary(scores, "sample.Scalar")
	if score == nil || score.State != facts.KnowledgeMeasured || score.Shallow == nil {
		t.Fatalf("stateless instance score = %#v", scores)
	}
}

func TestJavaDepthCreationUsesOneFamilyForOverloads(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	path := "src/main/java/sample/Created.java"
	writeSource(t, root, path, `package sample;
public final class Created {
  public Created() {}
  public Created(int value) {}
}`)
	program, err := adapter.Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "sample.Created")
	if boundary.State != facts.KnowledgeMeasured || boundary.Creation == nil {
		t.Fatalf("creation boundary = %#v", boundary)
	}
	if got := boundary.Creation.Family; got != "create:sample.Created" {
		t.Fatalf("creation family = %q", got)
	}
	var creationFamily *facts.RouteFamily
	for index := range boundary.RouteFamilies {
		if boundary.RouteFamilies[index].ID == "create:sample.Created" {
			creationFamily = &boundary.RouteFamilies[index]
		}
	}
	if creationFamily == nil || len(creationFamily.Routes) != 2 {
		t.Fatalf("creation routes = %#v", boundary.RouteFamilies)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "sample.Created")
	if score == nil || score.State != facts.KnowledgeNotApplicable || score.Shallow != nil {
		t.Fatalf("passive creation score = %#v", score)
	}
}

func TestJavaDepthPackageTypeUsesPackageAudience(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	path := "src/main/java/sample/PackageScalar.java"
	writeSource(t, root, path, `package sample;
final class PackageScalar {
  PackageScalar() {}
  int add(int value) { return value + 1; }
}`)
	program, err := adapter.Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "sample.PackageScalar")
	if boundary.Identity.Audience != "package" || boundary.State != facts.KnowledgeMeasured {
		t.Fatalf("package audience boundary = %#v", boundary)
	}
}

func scoreForJavaBoundary(scores []metrics.DepthScore, symbol string) *metrics.DepthScore {
	for index := range scores {
		if scores[index].Boundary.Symbol == symbol {
			return &scores[index]
		}
	}
	return nil
}
