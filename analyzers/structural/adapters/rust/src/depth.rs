use crate::model::{DepthFacts, FileFailure};
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet, VecDeque};
#[path = "depth_config.rs"]
mod config;
#[path = "depth_index.rs"]
mod index;
#[path = "depth_lowering.rs"]
mod lowering;
#[path = "depth_passive.rs"]
mod passive;
#[path = "depth_types.rs"]
mod types;

use config::{attrs_cfg, CfgState};
use index::{collect_scope, function_id, likely_crate_root, resolve_path};
use lowering::lower_function;
use passive::{passive_result_carrier, passive_value_enum, passive_value_object};
use types::{argument_type, concept_type, result_type, rust_signature};

struct FunctionInfo<'a> {
    id: String,
    module: Vec<String>,
    function: &'a syn::ItemFn,
    external: bool,
    cfg: CfgState,
}

struct Alias {
    module: Vec<String>,
    name: String,
    target: Vec<String>,
    public: bool,
    unresolved: bool,
}

struct Index<'a> {
    artifact: String,
    functions: Vec<FunctionInfo<'a>>,
    aliases: Vec<Alias>,
    unknown_external: bool,
    unsupported_public_item: bool,
    unknown_cfg_external: bool,
    shadowed_primitive: bool,
}

struct Lowered {
    flow: Value,
    supported: bool,
    reason: Option<&'static str>,
    calls: Vec<String>,
}

pub fn collect(parsed: &[(String, syn::File)], failures: &[FileFailure]) -> DepthFacts {
    let mut result = DepthFacts::default();
    let has_explicit_crate_root = parsed.iter().any(|(path, _)| likely_crate_root(path));
    for (path, syntax) in parsed {
        let mut index = Index {
            artifact: path.clone(),
            functions: Vec::new(),
            aliases: Vec::new(),
            unknown_external: false,
            unsupported_public_item: false,
            unknown_cfg_external: false,
            shadowed_primitive: false,
        };
        match attrs_cfg(&syntax.attrs) {
            CfgState::Disabled => {}
            CfgState::Unknown => {
                index.unknown_cfg_external = true;
                collect_scope(&mut index, &syntax.items, &[], true, path);
            }
            CfgState::Enabled => collect_scope(&mut index, &syntax.items, &[], true, path),
        }
        let passive = if failures.is_empty()
            && !index.unknown_external
            && !index.unknown_cfg_external
            && !index.shadowed_primitive
            && (!has_explicit_crate_root || likely_crate_root(path))
        {
            passive_result_carrier(path, syntax)
                .or_else(|| passive_value_enum(path, syntax))
                .or_else(|| passive_value_object(path, syntax))
        } else {
            None
        };
        let (boundaries, flow) =
            lower_index(path, &index, failures, has_explicit_crate_root, passive);
        result.boundaries.extend(boundaries);
        result.flows.push(flow);
    }
    result
}

fn lower_index(
    path: &str,
    index: &Index<'_>,
    failures: &[FileFailure],
    crate_root_context: bool,
    passive: Option<Value>,
) -> (Vec<Value>, Value) {
    let identity =
        json!({"artifact": path, "audience": "external", "view": "namespace", "symbol": path});
    let mut by_id = BTreeMap::new();
    for (position, function) in index.functions.iter().enumerate() {
        if function.cfg == CfgState::Enabled {
            by_id.insert(function.id.clone(), position);
        }
    }
    let mut lowered = Vec::with_capacity(index.functions.len());
    for function in &index.functions {
        if function.cfg == CfgState::Enabled {
            lowered.push(lower_function(function, index, &by_id));
        }
    }
    let mut lowered_by_id = BTreeMap::new();
    for item in &lowered {
        lowered_by_id.insert(
            item.flow["id"].as_str().unwrap_or_default().to_owned(),
            item,
        );
    }
    let roots: Vec<String> = index
        .functions
        .iter()
        .filter(|function| function.external && function.cfg == CfgState::Enabled)
        .map(|function| function.id.clone())
        .collect();
    let mut reachable = BTreeSet::new();
    let mut queue = VecDeque::from(roots);
    while let Some(id) = queue.pop_front() {
        if !reachable.insert(id.clone()) {
            continue;
        }
        if let Some(item) = lowered_by_id.get(&id) {
            for target in &item.calls {
                if by_id.contains_key(target) {
                    queue.push_back(target.clone());
                }
            }
        }
    }

    let mut routes = Vec::new();
    let mut families: BTreeMap<String, Vec<Value>> = BTreeMap::new();
    let mut slots = Vec::new();
    let mut concepts = BTreeSet::new();
    let mut reasons = Vec::new();
    let mut supported = failures.is_empty();
    if !failures.is_empty() {
        reasons.push(reason("incomplete_source_inventory"));
    }
    if index.unknown_external {
        supported = false;
        reasons.push(reason("unresolved_external_module"));
    }
    if index.unknown_cfg_external {
        supported = false;
        reasons.push(reason("unknown_cfg_selection"));
    }
    if index.unsupported_public_item {
        supported = false;
        reasons.push(reason("unsupported_public_item"));
    }
    if index.shadowed_primitive {
        supported = false;
        reasons.push(reason("shadowed_primitive_type"));
    }
    if crate_root_context && !likely_crate_root(path) {
        supported = false;
        reasons.push(reason("unknown_module_visibility"));
    }
    for function in &index.functions {
        if !function.external || function.cfg != CfgState::Enabled {
            continue;
        }
        let Some(lowered) = lowered_by_id.get(&function.id) else {
            supported = false;
            reasons.push(reason("unresolved_public_function"));
            continue;
        };
        if reachable.contains(&function.id) && !lowered.supported {
            supported = false;
            if let Some(code) = lowered.reason {
                reasons.push(reason(code));
            }
        }
        let route = make_route(function, &identity, &mut slots, &mut concepts);
        routes.push(route.clone());
        families.entry(function.id.clone()).or_default().push(route);
    }
    for alias in &index.aliases {
        if !alias.public {
            continue;
        }
        if alias.unresolved {
            supported = false;
            reasons.push(reason("unresolved_reexport"));
            continue;
        }
        let Some(target) = resolve_path(&alias.module, &alias.target, index, &by_id) else {
            supported = false;
            reasons.push(reason("unresolved_reexport"));
            continue;
        };
        let Some(function) = index.functions.iter().find(|item| item.id == target) else {
            supported = false;
            reasons.push(reason("unresolved_reexport"));
            continue;
        };
        let route_id = function_id(path, &alias.module, &alias.name);
        let route = make_route_with_id(&route_id, function, &identity, &mut slots, &mut concepts);
        routes.push(route.clone());
        families.entry(target).or_default().push(route);
    }
    if routes.is_empty() {
        supported = false;
        reasons.push(reason("unresolved_or_absent_public_surface"));
    }
    deduplicate_reasons(&mut reasons);
    let state = if supported { "measured" } else { "partial" };
    let mut boundary = json!({"identity": identity, "state": state, "knowledge": {"inventory": {"state": state, "essential": true}, "burden": {"state": state, "essential": true}, "behavior": {"state": state, "essential": true}, "alias_effects": {"state": state, "essential": true}}, "burden": {"O": families.len(), "T": concepts.len()}, "concepts": concepts.iter().map(|value| json!({"id": value, "kind": value})).collect::<Vec<_>>(), "slots": slots, "route_families": families.iter().map(|(id, routes)| json!({"id": id, "routes": routes})).collect::<Vec<_>>(), "reasons": reasons, "files": [path]});
    if let Some(evidence) = passive {
        boundary["evidence"] = json!([evidence]);
    }
    let flow = json!({"artifact": path, "language": "rust", "build_selection": "inferred", "functions": lowered.into_iter().map(|item| item.flow).collect::<Vec<_>>(), "public_routes": routes});
    (vec![boundary], flow)
}

fn make_route(
    function: &FunctionInfo<'_>,
    identity: &Value,
    slots: &mut Vec<Value>,
    concepts: &mut BTreeSet<String>,
) -> Value {
    make_route_with_id(&function.id, function, identity, slots, concepts)
}
fn make_route_with_id(
    route_id: &str,
    function: &FunctionInfo<'_>,
    identity: &Value,
    slots: &mut Vec<Value>,
    concepts: &mut BTreeSet<String>,
) -> Value {
    let mut required = Vec::new();
    for (position, argument) in function.function.sig.inputs.iter().enumerate() {
        let concept = argument_type(argument)
            .and_then(concept_type)
            .map(|value| value.0)
            .unwrap_or("unknown");
        concepts.insert(concept.to_owned());
        let slot = format!("{route_id}/arg{position}");
        required.push(slot.clone());
        slots.push(json!({"id": slot, "concept": concept, "required": true}));
    }
    if let Some(result) = result_type(function.function).and_then(concept_type) {
        concepts.insert(result.0.to_owned());
    }
    json!({"id": route_id, "family": function.id, "target_function_id": function.id, "boundary": identity, "signature": rust_signature(function.function), "required_slots": required, "exposed_slots": required})
}

fn reason(code: &str) -> Value {
    json!({"code": code, "dimension": "behavior", "message": code})
}
fn deduplicate_reasons(reasons: &mut Vec<Value>) {
    let mut seen = BTreeSet::new();
    reasons.retain(|item| seen.insert(item["code"].as_str().unwrap_or_default().to_owned()));
}

#[cfg(test)]
mod type_tests {
    use super::*;

    #[test]
    fn proves_private_field_passive_result_carrier() {
        let source = syn::parse_file(
            "pub struct Vessel { left:i32, right:bool } impl Vessel {
             pub fn new(left:i32,right:bool)->Self{Self{left,right}}
             pub fn left(&self)->i32{self.left}
             pub fn right(&self)->bool{self.right}
             pub fn replace(&mut self,left:i32){self.left=left}
             pub fn reset(&mut self){self.left=0;self.right=false}
            }",
        )
        .unwrap();
        let depth = collect(&[("lib.rs".into(), source)], &[]);
        let evidence = depth.boundaries[0]["evidence"].as_array().unwrap();
        assert_eq!(evidence.len(), 1);
        assert_eq!(evidence[0]["kind"], "passive-result-carrier-v1");
        assert_eq!(evidence[0]["status"], "proven");
        assert_eq!(evidence[0]["details"]["instance_fields"], 2);
        assert_eq!(evidence[0]["details"]["direct_getters"], 2);
        assert_eq!(evidence[0]["details"]["direct_mutators"], 2);
        assert_eq!(evidence[0]["details"]["constructors"], 1);
    }

    #[test]
    fn rejects_carrier_when_exposed_behavior_is_not_passive() {
        let sources = [
            "pub struct Vessel { value:i32 } impl Vessel { pub fn value(&self)->i32{if self.value>0{self.value}else{0}} }",
            "pub struct Vessel { value:i32 } impl Vessel { pub fn value(&self)->i32{self.value+1} }",
            "pub struct Vessel { value:i32 } impl Vessel { pub fn value(&self)->i32{self.value} pub fn add(&mut self,delta:i32){self.value+=delta} }",
            "pub struct Vessel { value:i32 } impl Vessel { pub fn value(&self)->i32{self.value} pub fn reload(&mut self){refresh()} } fn refresh(){}",
            "pub struct Vessel { pub value:i32 } impl Vessel { pub fn value(&self)->i32{self.value} }",
            "pub struct Vessel { value:i32 } impl Vessel { pub fn value(&self)->i32{self.value} } pub fn other(x:i32)->i32{x}",
            "pub struct Vessel { value:i32 } impl Vessel { pub fn value(&self)->i32{self.value} } mod nested { pub fn other(){} }",
            "#[derive(Clone)] pub struct Vessel { value:i32 } impl Vessel { pub fn value(&self)->i32{self.value} }",
            "pub struct Vessel { left:i32, right:i32 } impl Vessel { pub fn first(&self)->i32{self.left} pub fn second(&self)->i32{self.left} }",
        ];
        for source in sources {
            let syntax = syn::parse_file(source).unwrap();
            let depth = collect(&[("lib.rs".into(), syntax)], &[]);
            assert!(
                depth.boundaries[0]["evidence"].is_null(),
                "unexpected proof: {source}"
            );
        }
    }

    #[test]
    fn proves_simple_public_value_enum() {
        let source = syn::parse_file(
            "pub enum Parcel { Empty, Pair(i32, bool), Named { code:i32, valid:bool } }",
        )
        .unwrap();
        let depth = collect(&[("lib.rs".into(), source)], &[]);
        let evidence = depth.boundaries[0]["evidence"].as_array().unwrap();
        assert_eq!(evidence.len(), 1);
        assert_eq!(evidence[0]["kind"], "passive-enum-v1");
        assert_eq!(evidence[0]["status"], "proven");
        assert_eq!(evidence[0]["details"]["variants"], 3);
        assert_eq!(evidence[0]["details"]["unit_variants"], 1);
        assert_eq!(evidence[0]["details"]["tuple_variants"], 1);
        assert_eq!(evidence[0]["details"]["named_variants"], 1);
        assert_eq!(evidence[0]["details"]["data_fields"], 4);
    }

    #[test]
    fn proves_simple_public_value_object() {
        let source = syn::parse_file("pub struct Point { pub x:i32, pub y:i32 }").unwrap();
        let depth = collect(&[("lib.rs".into(), source)], &[]);
        let evidence = depth.boundaries[0]["evidence"].as_array().unwrap();
        assert_eq!(evidence.len(), 1);
        assert_eq!(evidence[0]["kind"], "passive-value-object-v1");
        assert_eq!(evidence[0]["status"], "proven");
        assert_eq!(evidence[0]["details"]["fields"], 2);
        assert_eq!(evidence[0]["details"]["public_fields"], 2);
    }

    #[test]
    fn rejects_value_object_with_behaviorful_impl() {
        let source = syn::parse_file(
            "pub struct Point { pub x:i32, pub y:i32 } impl Point { pub fn distance(&self)->i32{self.x+self.y} }",
        )
        .unwrap();
        let depth = collect(&[("lib.rs".into(), source)], &[]);
        assert!(depth.boundaries[0]["evidence"].is_null());
    }

    #[test]
    fn rejects_behaviorful_or_uncertain_public_enum() {
        let sources = [
            "pub enum Parcel { Empty, Pair(i32) } impl Parcel { pub fn value(&self)->i32{1} }",
            "pub enum Parcel { Empty, Pair(i32) } impl Display for Parcel {}",
            "#[derive(Clone)] pub enum Parcel { Empty, Pair(i32) }",
            "#[cfg(feature=\"parcel\")] pub enum Parcel { Empty, Pair(i32) }",
            "pub enum Parcel { Pair(Vec<i32>) }",
            "pub enum Parcel { Empty = 1, Pair(i32) }",
            "pub enum Parcel { Empty } pub fn other(){}",
        ];
        for source in sources {
            let syntax = syn::parse_file(source).unwrap();
            let depth = collect(&[("lib.rs".into(), syntax)], &[]);
            assert!(
                depth.boundaries[0]["evidence"].is_null(),
                "unexpected proof: {source}"
            );
        }
    }

    #[test]
    fn bool_not_is_a_typed_primitive() {
        let source = syn::parse_file("pub fn f(x:bool)->bool{!x}").unwrap();
        let depth = collect(&[("lib.rs".into(), source)], &[]);
        assert_eq!(depth.boundaries[0]["state"], "measured");
        assert_eq!(
            depth.flows[0]["functions"][0]["blocks"][0]["instructions"][0]["operator"],
            "!"
        );
    }
    #[test]
    fn cfg_any_empty_is_not_an_export() {
        let source = syn::parse_file("#[cfg(any())] pub fn f(x:bool)->bool{x}").unwrap();
        let depth = collect(&[("lib.rs".into(), source)], &[]);
        assert_eq!(depth.boundaries[0]["state"], "partial");
        assert_eq!(
            depth.boundaries[0]["reasons"][0]["code"],
            "unresolved_or_absent_public_surface"
        );
    }
    #[test]
    fn cfg_attr_keeps_selection_uncertain() {
        let source =
            syn::parse_file("#[cfg_attr(all(), cfg(any()))] pub fn f(x:bool)->bool{x}").unwrap();
        let depth = collect(&[("lib.rs".into(), source)], &[]);
        assert_eq!(depth.boundaries[0]["state"], "partial");
        assert_eq!(
            depth.boundaries[0]["reasons"][0]["code"],
            "unknown_cfg_selection"
        );
    }
    #[test]
    fn non_root_file_is_partial_in_multi_file_inventory() {
        let root = syn::parse_file("pub fn f(x:bool)->bool{x}").unwrap();
        let module = syn::parse_file("pub fn helper(x:bool)->bool{x}").unwrap();
        let depth = collect(
            &[("lib.rs".into(), root), ("helper.rs".into(), module)],
            &[],
        );
        let boundary = depth
            .boundaries
            .iter()
            .find(|item| item["identity"]["artifact"] == "helper.rs")
            .unwrap();
        assert_eq!(boundary["state"], "partial");
        assert_eq!(boundary["reasons"][0]["code"], "unknown_module_visibility");
    }
    #[test]
    fn unused_private_unsupported_function_does_not_poison_root() {
        let source =
            syn::parse_file("fn unused(x:Vec<i32>)->Vec<i32>{x} pub fn f(x:bool)->bool{x}")
                .unwrap();
        let depth = collect(&[("lib.rs".into(), source)], &[]);
        assert_eq!(depth.boundaries[0]["state"], "measured");
    }
}
