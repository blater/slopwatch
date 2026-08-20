export interface ComponentDefinition {
  component_id: string;
  definition_version: string;
  axis: "structural_core" | "typescript_type_safety";
  taxonomy: string;
  kind: "continuous" | "finding";
  scope: "function" | "expression";
  evidence_posture: "oracle_aligned" | "supported";
  support_level: "conformant" | "supported";
  required_capability: "syntax" | "types";
  deduplication_key: string;
}

export const COMPONENTS = [
  {
    component_id: "cognitive_complexity",
    definition_version: "pmd-sonar-v1",
    axis: "structural_core",
    taxonomy: "structural.complexity",
    kind: "continuous",
    scope: "function",
    evidence_posture: "oracle_aligned",
    support_level: "conformant",
    required_capability: "syntax",
    deduplication_key: "canonical-file,function-start"
  },
  {
    component_id: "cyclomatic_method_complexity",
    definition_version: "pmd-v1",
    axis: "structural_core",
    taxonomy: "structural.complexity",
    kind: "continuous",
    scope: "function",
    evidence_posture: "oracle_aligned",
    support_level: "conformant",
    required_capability: "syntax",
    deduplication_key: "canonical-file,function-start"
  },
  {
    component_id: "npath_complexity",
    definition_version: "pmd-v1",
    axis: "structural_core",
    taxonomy: "structural.complexity",
    kind: "continuous",
    scope: "function",
    evidence_posture: "oracle_aligned",
    support_level: "conformant",
    required_capability: "syntax",
    deduplication_key: "canonical-file,function-start"
  },
  {
    component_id: "deeply_nested_if",
    definition_version: "pmd-v1",
    axis: "structural_core",
    taxonomy: "structural.nesting",
    kind: "finding",
    scope: "expression",
    evidence_posture: "oracle_aligned",
    support_level: "conformant",
    required_capability: "syntax",
    deduplication_key: "canonical-file,start,end"
  },
  ...[
    "explicit_any",
    "unsafe_type_assertion",
    "unsafe_type_propagation",
    "unsafe_type_use",
    "unsafe_type_boundary",
    "ambiguous_boolean_expression",
    "non_exhaustive_union"
  ].map((component_id) => ({
    component_id,
    definition_version: "typescript-local-sink-v1",
    axis: "typescript_type_safety" as const,
    taxonomy: "typescript.type_safety",
    kind: "finding" as const,
    scope: "expression" as const,
    evidence_posture: "supported" as const,
    support_level: "supported" as const,
    required_capability: "types" as const,
    deduplication_key: "component,canonical-file,start,end,normalized-symbol"
  }))
] satisfies ComponentDefinition[];

export const COMPONENT_BY_ID = new Map(COMPONENTS.map((item) => [item.component_id, item]));
export const STRUCTURAL_COMPONENTS = new Set(
  COMPONENTS.filter((item) => item.required_capability === "syntax").map((item) => item.component_id)
);
export const TYPED_COMPONENTS = new Set(
  COMPONENTS.filter((item) => item.required_capability === "types").map((item) => item.component_id)
);
