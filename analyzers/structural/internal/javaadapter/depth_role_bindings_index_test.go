package javaadapter

import (
	"strings"
	"testing"
)

func TestJavaSupportingContractIndexedSlotsPreserveGenericFirstUse(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	path := "src/main/java/sample/Wiring.java"
	writeSource(t, root, path, `package sample;
interface Contract<T> { T map(T value); int count(); }
final class Provider implements Contract<String> {
  public String map(String value) { return value; }
  public int count() { return 1; }
}
final class Unused implements Contract<String> {
  public String map(String value) { return value; }
  public int count() { return 2; }
}
final class Wiring {
  static final Provider ROOT = new Provider();
  static final Contract<String> FIRST = ROOT;
  static final Contract<String> SECOND = FIRST;
  static Consumer make() { return new Consumer(SECOND); }
}
final class Consumer {
  private final Contract<String> value;
  Consumer() { this(Wiring.SECOND); }
  Consumer(Contract<String> input) { value = input; }
  String use(String input) { int size = value.count(); return value.map(input); }
}`)
	program, err := adapter.Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	role := javaDepthBoundaryBySymbol(t, program, "sample.Provider").SupportingContract
	if role == nil || len(role.Bindings) != 1 {
		t.Fatalf("indexed constructor binding = %#v", role)
	}
	binding := role.Bindings[0]
	if binding.ContractMember != "sample.Contract#count()" || !strings.Contains(binding.Use, "sample.Consumer#use(java.lang.String)") {
		t.Fatalf("first invocation evidence changed: %#v", binding)
	}
	unused := javaDepthBoundaryBySymbol(t, program, "sample.Unused").SupportingContract
	if unused == nil || len(unused.Bindings) != 0 {
		t.Fatalf("unconstructed implementation gained bindings: %#v", unused)
	}
}

func TestJavaSupportingContractAliasCyclesDoNotInventConstruction(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	path := "src/main/java/sample/Wiring.java"
	writeSource(t, root, path, `package sample;
interface Contract { int count(); }
final class Provider implements Contract { public int count() { return 1; } }
final class Wiring {
  static final Contract FIRST = Wiring.SECOND;
  static final Contract SECOND = Wiring.FIRST;
  static Consumer first() { return new Consumer(FIRST); }
  static Consumer second() { return new Consumer(SECOND); }
}
final class Consumer {
  private final Contract value;
  Consumer(Contract input) { value = input; }
  int use() { return value.count(); }
}`)
	program, err := adapter.Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	role := javaDepthBoundaryBySymbol(t, program, "sample.Provider").SupportingContract
	if role == nil || len(role.Bindings) != 0 {
		t.Fatalf("cyclic alias invented implementation: %#v", role)
	}
}

func TestJavaSupportingContractIndexedSameArityOverloads(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	path := "src/main/java/sample/Wiring.java"
	writeSource(t, root, path, `package sample;
interface Base<T> { T map(T value); }
interface Contract extends Base<String> { int map(Integer value); int map(Long value); }
final class Provider implements Contract {
  public String map(String value) { return value; }
  public int map(Integer value) { return value; }
  public int map(Long value) { return value.intValue(); }
}
final class Wiring { static Consumer make() { return new Consumer(new Provider()); } }
final class Consumer {
  private final Contract value;
  Consumer(Contract input) { value = input; }
  String use(String input) { return value.map(input); }
}`)
	program, err := adapter.Analyze(root, []string{path}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	role := javaDepthBoundaryBySymbol(t, program, "sample.Provider").SupportingContract
	if role == nil || len(role.Members) != 3 || len(role.Bindings) != 1 {
		t.Fatalf("overload role = %#v", role)
	}
	if role.Bindings[0].ContractMember != "sample.Base#map(T)" {
		t.Fatalf("generic inherited overload resolved incorrectly: %#v", role.Bindings)
	}
}
