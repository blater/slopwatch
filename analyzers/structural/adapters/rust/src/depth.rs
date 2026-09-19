use crate::model::{DepthFacts, FileFailure};
use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet, VecDeque};
use syn::{Expr, FnArg, Item, Meta, ReturnType, Stmt, Type, UseTree, Visibility};

#[derive(Clone, Copy, PartialEq, Eq)]
enum CfgState {
    Enabled,
    Disabled,
    Unknown,
}

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

fn collect_scope<'a>(
    index: &mut Index<'a>,
    items: &'a [Item],
    module: &[String],
    external: bool,
    path: &str,
) {
    for item in items {
        let cfg = item_cfg(item);
        if cfg == CfgState::Disabled {
            continue;
        }
        match item {
            Item::Fn(function) => {
                let public = matches!(function.vis, Visibility::Public(_));
                let externally_visible = external && public;
                if cfg == CfgState::Unknown && externally_visible {
                    index.unknown_cfg_external = true;
                }
                index.functions.push(FunctionInfo {
                    id: function_id(path, module, &function.sig.ident.to_string()),
                    module: module.to_vec(),
                    function,
                    external: externally_visible,
                    cfg,
                });
            }
            Item::Mod(value) => {
                let child_external = external && matches!(value.vis, Visibility::Public(_));
                if cfg == CfgState::Unknown && child_external {
                    index.unknown_cfg_external = true;
                }
                if cfg == CfgState::Unknown {
                    continue;
                }
                if let Some((_, nested)) = &value.content {
                    let mut child = module.to_vec();
                    child.push(value.ident.to_string());
                    collect_scope(index, nested, &child, child_external, path);
                } else if child_external {
                    index.unknown_external = true;
                }
            }
            Item::Use(value) => {
                if cfg == CfgState::Unknown {
                    if external && matches!(value.vis, Visibility::Public(_)) {
                        index.unknown_cfg_external = true;
                    }
                    continue;
                }
                let mut aliases = Vec::new();
                collect_use_tree(&value.tree, &[], &mut aliases);
                for (name, target, unresolved) in aliases {
                    if matches!(name.as_str(), "i32" | "i64" | "bool") {
                        index.shadowed_primitive = true;
                    }
                    index.aliases.push(Alias {
                        module: module.to_vec(),
                        name,
                        target,
                        public: matches!(value.vis, Visibility::Public(_)) && external,
                        unresolved,
                    });
                }
            }
            Item::Struct(value) if external && matches!(value.vis, Visibility::Public(_)) => {
                index.unsupported_public_item = true
            }
            Item::Enum(value) if external && matches!(value.vis, Visibility::Public(_)) => {
                index.unsupported_public_item = true
            }
            Item::Trait(value) if external && matches!(value.vis, Visibility::Public(_)) => {
                index.unsupported_public_item = true
            }
            Item::Type(value) if external && matches!(value.vis, Visibility::Public(_)) => {
                index.unsupported_public_item = true
            }
            Item::Const(value) if external && matches!(value.vis, Visibility::Public(_)) => {
                index.unsupported_public_item = true
            }
            Item::Static(value) if external && matches!(value.vis, Visibility::Public(_)) => {
                index.unsupported_public_item = true
            }
            Item::Macro(_) if external => index.unsupported_public_item = true,
            Item::Impl(_) if external => index.unsupported_public_item = true,
            _ => {}
        }
        let shadow = match item {
            Item::Struct(value) => value.ident.to_string(),
            Item::Enum(value) => value.ident.to_string(),
            Item::Type(value) => value.ident.to_string(),
            Item::Trait(value) => value.ident.to_string(),
            _ => String::new(),
        };
        if matches!(shadow.as_str(), "i32" | "i64" | "bool") {
            index.shadowed_primitive = true;
        }
    }
}

fn collect_use_tree(
    tree: &UseTree,
    prefix: &[String],
    output: &mut Vec<(String, Vec<String>, bool)>,
) {
    match tree {
        UseTree::Path(value) => {
            let mut next = prefix.to_vec();
            next.push(value.ident.to_string());
            collect_use_tree(&value.tree, &next, output);
        }
        UseTree::Name(value) => {
            let mut target = prefix.to_vec();
            target.push(value.ident.to_string());
            output.push((value.ident.to_string(), target, false));
        }
        UseTree::Rename(value) => {
            let mut target = prefix.to_vec();
            target.push(value.ident.to_string());
            output.push((value.rename.to_string(), target, false));
        }
        UseTree::Group(value) => {
            for item in &value.items {
                collect_use_tree(item, prefix, output);
            }
        }
        UseTree::Glob(_) => output.push((String::new(), prefix.to_vec(), true)),
    }
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

struct PassiveCarrierCounts {
    fields: usize,
    getters: usize,
    mutators: usize,
    constructors: usize,
}

fn passive_result_carrier(path: &str, syntax: &syn::File) -> Option<Value> {
    if !syntax.attrs.is_empty() {
        return None;
    }
    let mut carrier = None;
    let mut implementations = Vec::new();
    let mut constants = BTreeMap::new();
    for item in &syntax.items {
        match item {
            Item::Struct(value)
                if matches!(value.vis, Visibility::Public(_)) && value.attrs.is_empty() =>
            {
                if carrier.is_some() {
                    return None;
                }
                carrier = Some(value);
            }
            Item::Impl(value) if value.attrs.is_empty() => implementations.push(value),
            Item::Const(value)
                if matches!(value.vis, Visibility::Inherited)
                    && value.attrs.is_empty()
                    && passive_constant_type(&value.ty).is_some()
                    && passive_constant_expr(&value.expr) =>
            {
                constants.insert(value.ident.to_string(), passive_type_key(&value.ty)?);
            }
            _ => return None,
        }
    }
    let carrier = carrier?;
    if !carrier.generics.params.is_empty() || carrier.generics.where_clause.is_some() {
        return None;
    }
    let syn::Fields::Named(fields) = &carrier.fields else {
        return None;
    };
    if fields.named.is_empty() || fields.named.len() > 256 {
        return None;
    }
    let mut field_types = BTreeMap::new();
    for field in &fields.named {
        if !matches!(field.vis, Visibility::Inherited) || !field.attrs.is_empty() {
            return None;
        }
        let name = field.ident.as_ref()?.to_string();
        field_types.insert(name, passive_type_key(&field.ty)?);
    }
    if implementations.is_empty() {
        return None;
    }
    let carrier_name = carrier.ident.to_string();
    let mut counts = PassiveCarrierCounts {
        fields: field_types.len(),
        getters: 0,
        mutators: 0,
        constructors: 0,
    };
    let mut getter_fields = BTreeSet::new();
    let mut public_methods = 0;
    for implementation in implementations {
        if implementation.trait_.is_some()
            || !implementation.generics.params.is_empty()
            || implementation.generics.where_clause.is_some()
            || !passive_self_type(&implementation.self_ty, &carrier_name)
        {
            return None;
        }
        for item in &implementation.items {
            match item {
                syn::ImplItem::Const(value)
                    if matches!(value.vis, Visibility::Inherited)
                        && value.attrs.is_empty()
                        && passive_constant_type(&value.ty).is_some()
                        && passive_constant_expr(&value.expr) =>
                {
                    constants.insert(value.ident.to_string(), passive_type_key(&value.ty)?);
                }
                syn::ImplItem::Method(method) if method.attrs.is_empty() => {
                    if !matches!(method.vis, Visibility::Public(_)) {
                        continue;
                    }
                    public_methods += 1;
                    if !passive_method_shape(method) {
                        return None;
                    }
                    let receiver = match method.sig.inputs.first() {
                        Some(FnArg::Receiver(receiver)) => receiver,
                        _ => {
                            if passive_constructor(method, &carrier_name, &field_types, &constants)
                            {
                                counts.constructors += 1;
                                continue;
                            }
                            return None;
                        }
                    };
                    let mutable_receiver = receiver.mutability.is_some();
                    if receiver.reference.is_none() {
                        return None;
                    }
                    if !mutable_receiver
                        && method.sig.inputs.len() == 1
                        && passive_getter(method, &field_types)
                            .map(|field| getter_fields.insert(field))
                            .unwrap_or(false)
                    {
                        counts.getters += 1;
                    } else if mutable_receiver && passive_mutator(method, &field_types, &constants)
                    {
                        counts.mutators += 1;
                    } else {
                        return None;
                    }
                }
                _ => return None,
            }
        }
    }
    if public_methods == 0 || getter_fields.len() != counts.fields {
        return None;
    }
    counts.getters = getter_fields.len();
    Some(json!({
        "id": format!("{path}/{carrier_name}/passive-result-carrier"),
        "kind": "passive-result-carrier-v1",
        "status": "proven",
        "details": {
            "instance_fields": counts.fields,
            "direct_getters": counts.getters,
            "direct_mutators": counts.mutators,
            "constructors": counts.constructors,
        },
        "provenance": [],
    }))
}

fn passive_value_enum(path: &str, syntax: &syn::File) -> Option<Value> {
    if !syntax.attrs.is_empty() || syntax.items.len() != 1 {
        return None;
    }
    let Item::Enum(value) = &syntax.items[0] else {
        return None;
    };
    if !matches!(value.vis, Visibility::Public(_))
        || !value.attrs.is_empty()
        || !value.generics.params.is_empty()
        || value.generics.where_clause.is_some()
        || value.variants.is_empty()
        || value.variants.len() > 256
    {
        return None;
    }
    let mut unit_variants = 0;
    let mut tuple_variants = 0;
    let mut named_variants = 0;
    let mut data_fields = 0;
    for variant in &value.variants {
        if !variant.attrs.is_empty() || variant.discriminant.is_some() {
            return None;
        }
        match &variant.fields {
            syn::Fields::Unit => unit_variants += 1,
            syn::Fields::Unnamed(fields) => {
                tuple_variants += 1;
                if fields.unnamed.len() > 256 {
                    return None;
                }
                for field in &fields.unnamed {
                    if !field.attrs.is_empty() || !passive_enum_type(&field.ty) {
                        return None;
                    }
                    data_fields += 1;
                }
            }
            syn::Fields::Named(fields) => {
                named_variants += 1;
                if fields.named.len() > 256 {
                    return None;
                }
                for field in &fields.named {
                    if !field.attrs.is_empty() || !passive_enum_type(&field.ty) {
                        return None;
                    }
                    data_fields += 1;
                }
            }
        }
    }
    Some(json!({
        "id": format!("{path}/{}/passive-enum", value.ident),
        "kind": "passive-enum-v1",
        "status": "proven",
        "details": {
            "variants": value.variants.len(),
            "unit_variants": unit_variants,
            "tuple_variants": tuple_variants,
            "named_variants": named_variants,
            "data_fields": data_fields,
        },
        "provenance": [],
    }))
}

fn passive_value_object(path: &str, syntax: &syn::File) -> Option<Value> {
    if !syntax.attrs.is_empty() || syntax.items.len() != 1 {
        return None;
    }
    let Item::Struct(value) = &syntax.items[0] else {
        return None;
    };
    if !matches!(value.vis, Visibility::Public(_))
        || !value.attrs.is_empty()
        || !value.generics.params.is_empty()
        || value.generics.where_clause.is_some()
    {
        return None;
    }
    let syn::Fields::Named(fields) = &value.fields else {
        return None;
    };
    if fields.named.is_empty() || fields.named.len() > 256 {
        return None;
    }
    for field in &fields.named {
        if !matches!(field.vis, Visibility::Public(_))
            || !field.attrs.is_empty()
            || !passive_enum_type(&field.ty)
        {
            return None;
        }
    }
    Some(json!({
        "id": format!("{path}/{}/passive-value-object", value.ident),
        "kind": "passive-value-object-v1",
        "status": "proven",
        "details": {
            "fields": fields.named.len(),
            "public_fields": fields.named.len(),
        },
        "provenance": [],
    }))
}

fn passive_enum_type(ty: &Type) -> bool {
    match ty {
        Type::Path(path) if path.qself.is_none() => path
            .path
            .segments
            .iter()
            .all(|segment| matches!(segment.arguments, syn::PathArguments::None)),
        Type::Reference(value) => passive_enum_type(&value.elem),
        Type::Tuple(value) => value.elems.iter().all(passive_enum_type),
        Type::Array(value) => passive_enum_type(&value.elem),
        Type::Slice(value) => passive_enum_type(&value.elem),
        Type::Paren(value) => passive_enum_type(&value.elem),
        _ => false,
    }
}

fn passive_self_type(ty: &Type, carrier: &str) -> bool {
    let Type::Path(path) = ty else { return false };
    path.qself.is_none()
        && path.path.segments.len() == 1
        && path.path.segments[0].arguments.is_none()
        && path.path.segments[0].ident == carrier
}

fn passive_method_shape(method: &syn::ImplItemMethod) -> bool {
    method.sig.asyncness.is_none()
        && method.sig.constness.is_none()
        && method.sig.unsafety.is_none()
        && method.sig.abi.is_none()
        && method.sig.generics.params.is_empty()
        && method.sig.generics.where_clause.is_none()
        && method.sig.variadic.is_none()
}

fn passive_constructor(
    method: &syn::ImplItemMethod,
    carrier: &str,
    fields: &BTreeMap<String, String>,
    constants: &BTreeMap<String, String>,
) -> bool {
    let ReturnType::Type(_, result) = &method.sig.output else {
        return false;
    };
    if !passive_self_type(result, carrier)
        && !matches!(result.as_ref(), Type::Path(path) if path.qself.is_none()
            && path.path.segments.len() == 1
            && path.path.segments[0].ident == "Self")
    {
        return false;
    }
    let mut parameters = BTreeMap::new();
    for input in &method.sig.inputs {
        let FnArg::Typed(value) = input else {
            return false;
        };
        let syn::Pat::Ident(pattern) = value.pat.as_ref() else {
            return false;
        };
        let Some(value_type) = passive_type_key(&value.ty) else {
            return false;
        };
        parameters.insert(pattern.ident.to_string(), value_type);
    }
    let Some(expression) = passive_single_expression(&method.block) else {
        return false;
    };
    let Expr::Struct(value) = expression else {
        return false;
    };
    let path = &value.path;
    if path.segments.len() != 1
        || !(path.segments[0].ident == carrier || path.segments[0].ident == "Self")
        || value.rest.is_some()
        || value.fields.len() != fields.len()
    {
        return false;
    }
    let mut seen = BTreeSet::new();
    for field in &value.fields {
        let syn::Member::Named(name) = &field.member else {
            return false;
        };
        let name = name.to_string();
        if !seen.insert(name.clone()) {
            return false;
        }
        let Some(expected) = fields.get(&name) else {
            return false;
        };
        if !passive_allowed_value(&field.expr, expected, &parameters, constants) {
            return false;
        }
    }
    seen.len() == fields.len()
}

fn passive_getter(
    method: &syn::ImplItemMethod,
    fields: &BTreeMap<String, String>,
) -> Option<String> {
    let ReturnType::Type(_, result) = &method.sig.output else {
        return None;
    };
    let result = passive_type_key(result);
    let expression = passive_single_expression(&method.block);
    let Some(field) = expression.and_then(passive_self_field) else {
        return None;
    };
    if result.as_ref() == fields.get(&field) {
        Some(field)
    } else {
        None
    }
}

fn passive_mutator(
    method: &syn::ImplItemMethod,
    fields: &BTreeMap<String, String>,
    constants: &BTreeMap<String, String>,
) -> bool {
    let mut parameters = BTreeMap::new();
    for input in method.sig.inputs.iter().skip(1) {
        let FnArg::Typed(value) = input else {
            return false;
        };
        let syn::Pat::Ident(pattern) = value.pat.as_ref() else {
            return false;
        };
        let Some(value_type) = passive_type_key(&value.ty) else {
            return false;
        };
        parameters.insert(pattern.ident.to_string(), value_type);
    }
    if method.block.stmts.is_empty() {
        return false;
    }
    for statement in &method.block.stmts {
        let expression = match statement {
            Stmt::Semi(expression, _) => expression,
            Stmt::Expr(expression) => expression,
            _ => return false,
        };
        let Expr::Assign(assignment) = expression else {
            return false;
        };
        let Some(field) = passive_self_field(&assignment.left) else {
            return false;
        };
        let Some(expected) = fields.get(&field) else {
            return false;
        };
        if !passive_allowed_value(&assignment.right, expected, &parameters, constants) {
            return false;
        }
    }
    true
}

fn passive_self_field(expression: &Expr) -> Option<String> {
    let Expr::Field(field) = expression else {
        return None;
    };
    let Expr::Path(base) = field.base.as_ref() else {
        return None;
    };
    if base.path.segments.len() != 1 || base.path.segments[0].ident != "self" {
        return None;
    }
    match &field.member {
        syn::Member::Named(name) => Some(name.to_string()),
        syn::Member::Unnamed(_) => None,
    }
}

fn passive_single_expression(block: &syn::Block) -> Option<&Expr> {
    if block.stmts.len() != 1 {
        return None;
    }
    match block.stmts.first()? {
        Stmt::Expr(expression) => Some(expression),
        Stmt::Semi(Expr::Return(value), _) => value.expr.as_deref(),
        _ => None,
    }
}

fn passive_allowed_value(
    expression: &Expr,
    expected: &str,
    parameters: &BTreeMap<String, String>,
    constants: &BTreeMap<String, String>,
) -> bool {
    match expression {
        Expr::Path(path) if path.path.segments.len() == 1 => {
            let name = path.path.segments[0].ident.to_string();
            parameters
                .get(&name)
                .map(|value| value == expected)
                .unwrap_or(false)
                || constants
                    .get(&name)
                    .map(|value| value == expected)
                    .unwrap_or(false)
        }
        Expr::Path(path)
            if path.path.segments.len() == 2 && path.path.segments[0].ident == "Self" =>
        {
            constants
                .get(&path.path.segments[1].ident.to_string())
                .map(|value| value == expected)
                .unwrap_or(false)
        }
        Expr::Lit(literal) => passive_literal_value(&literal.lit, expected),
        Expr::Unary(value) if matches!(value.op, syn::UnOp::Neg(_)) => {
            matches!(value.expr.as_ref(), Expr::Lit(literal) if passive_integer_type(expected) && matches!(literal.lit, syn::Lit::Int(_)))
        }
        _ => false,
    }
}

fn passive_constant_type(ty: &Type) -> Option<String> {
    passive_type_key(ty)
}

fn passive_constant_expr(expression: &Expr) -> bool {
    matches!(expression, Expr::Lit(_))
        || matches!(expression, Expr::Unary(value) if matches!(value.op, syn::UnOp::Neg(_)) && matches!(value.expr.as_ref(), Expr::Lit(_)))
}

fn passive_literal_value(literal: &syn::Lit, expected: &str) -> bool {
    match literal {
        syn::Lit::Bool(_) => expected == "bool",
        syn::Lit::Int(_) => passive_integer_type(expected),
        syn::Lit::Float(_) => matches!(expected, "f32" | "f64"),
        syn::Lit::Str(_) => matches!(expected, "String" | "str"),
        syn::Lit::Byte(_) => expected == "u8",
        syn::Lit::ByteStr(_) => expected.contains("[u8]"),
        _ => false,
    }
}

fn passive_integer_type(ty: &str) -> bool {
    matches!(
        ty,
        "i8" | "i16"
            | "i32"
            | "i64"
            | "i128"
            | "isize"
            | "u8"
            | "u16"
            | "u32"
            | "u64"
            | "u128"
            | "usize"
    )
}

fn passive_type_key(ty: &Type) -> Option<String> {
    match ty {
        Type::Path(path) if path.qself.is_none() => Some(
            path.path
                .segments
                .iter()
                .map(|segment| segment.ident.to_string())
                .collect::<Vec<_>>()
                .join("::"),
        ),
        Type::Reference(value) => Some(format!(
            "&{}{}",
            if value.mutability.is_some() {
                "mut "
            } else {
                ""
            },
            passive_type_key(&value.elem)?
        )),
        Type::Tuple(value) => Some(format!(
            "({})",
            value
                .elems
                .iter()
                .map(passive_type_key)
                .collect::<Option<Vec<_>>>()?
                .join(",")
        )),
        Type::Array(value) => Some(format!("[{}]", passive_type_key(&value.elem)?)),
        Type::Slice(value) => Some(format!("[{}]", passive_type_key(&value.elem)?)),
        Type::Paren(value) => passive_type_key(&value.elem),
        _ => None,
    }
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

fn lower_function(
    function: &FunctionInfo<'_>,
    index: &Index<'_>,
    by_id: &BTreeMap<String, usize>,
) -> Lowered {
    let item = function.function;
    if item.sig.asyncness.is_some()
        || item.sig.constness.is_some()
        || item.sig.unsafety.is_some()
        || item.sig.abi.is_some()
        || !item.sig.generics.params.is_empty()
        || item.sig.generics.where_clause.is_some()
    {
        return unsupported_flow(&function.id, item, "unsupported_signature_shape");
    }
    let mut formals = Vec::new();
    let mut values = BTreeMap::new();
    let mut value_types = BTreeMap::new();
    for (position, argument) in item.sig.inputs.iter().enumerate() {
        let Some(ty) = argument_type(argument) else {
            return unsupported_flow(&function.id, item, "unsupported_signature_shape");
        };
        let Some((_, name)) = concept_type(ty) else {
            return unsupported_flow(&function.id, item, "unsupported_surface_type");
        };
        let id = format!("arg{position}");
        formals.push(json!({"id": id, "path": format!("{}/arg{position}", function.id), "type": name, "value_kind": value_kind(ty)}));
        if let Some(argument_name) = argument_name(argument) {
            values.insert(argument_name.clone(), id);
            value_types.insert(argument_name, name);
        }
    }
    let Some(result) = result_type(item) else {
        return unsupported_flow(&function.id, item, "unsupported_signature_shape");
    };
    let Some((_, result_name)) = concept_type(result) else {
        return unsupported_flow(&function.id, item, "unsupported_surface_type");
    };
    let mut instructions = Vec::new();
    let mut returned = None;
    let mut calls = Vec::new();
    for (position, statement) in item.block.stmts.iter().enumerate() {
        let last = position + 1 == item.block.stmts.len();
        match statement {
            Stmt::Local(local) => {
                let Some(name) = local_name(local) else {
                    return unsupported_flow_with_calls(
                        &function.id,
                        item,
                        "unsupported_local_binding",
                        calls,
                    );
                };
                let Some((_, initializer)) = local.init.as_ref() else {
                    return unsupported_flow_with_calls(
                        &function.id,
                        item,
                        "unsupported_local_binding",
                        calls,
                    );
                };
                let Some((value, value_type)) = lower_expression(
                    initializer,
                    function,
                    index,
                    by_id,
                    &mut instructions,
                    &values,
                    &value_types,
                    None,
                    &mut calls,
                ) else {
                    return unsupported_flow_with_calls(
                        &function.id,
                        item,
                        "unsupported_expression",
                        calls,
                    );
                };
                values.insert(name.clone(), value);
                value_types.insert(name, value_type);
            }
            Stmt::Semi(Expr::Return(value), _) | Stmt::Expr(Expr::Return(value)) if last => {
                returned = value.expr.as_deref()
            }
            Stmt::Expr(expression) if last => returned = Some(expression),
            _ => {
                return unsupported_flow_with_calls(
                    &function.id,
                    item,
                    "unsupported_control_flow",
                    calls,
                )
            }
        }
    }
    let Some(expression) = returned else {
        return unsupported_flow_with_calls(&function.id, item, "unsupported_control_flow", calls);
    };
    let operand = match lower_expression(
        expression,
        function,
        index,
        by_id,
        &mut instructions,
        &values,
        &value_types,
        Some(result_name.as_str()),
        &mut calls,
    ) {
        Some((value, value_type)) if value_type == result_name => value,
        _ => {
            return unsupported_flow_with_calls(&function.id, item, "unsupported_expression", calls)
        }
    };
    instructions.push(json!({"id": "return", "opcode": "return", "operands": [operand]}));
    Lowered {
        flow: flow_json(&function.id, &formals, result, &instructions),
        supported: true,
        reason: None,
        calls,
    }
}

fn unsupported_flow(id: &str, function: &syn::ItemFn, reason: &'static str) -> Lowered {
    unsupported_flow_with_calls(id, function, reason, Vec::new())
}
fn unsupported_flow_with_calls(
    id: &str,
    function: &syn::ItemFn,
    reason: &'static str,
    calls: Vec<String>,
) -> Lowered {
    let fallback = Type::Verbatim(proc_macro2::TokenStream::new());
    let result = result_type(function).unwrap_or(&fallback);
    Lowered {
        flow: flow_json(
            id,
            &[],
            result,
            &[
                json!({"id": "n1", "opcode": "unknown", "type": "unknown", "value_kind": "unknown", "results": ["n1"]}),
                json!({"id": "return", "opcode": "return", "operands": ["n1"]}),
            ],
        ),
        supported: false,
        reason: Some(reason),
        calls,
    }
}
fn flow_json(id: &str, formals: &[Value], result: &Type, instructions: &[Value]) -> Value {
    let (_, result_name) = concept_type(result).unwrap_or(("unknown", "unknown".to_owned()));
    json!({"id": id, "formals": formals, "results": [{"id": "result0", "type": result_name, "value_kind": value_kind(result)}], "entry": "entry", "blocks": [{"id": "entry", "instructions": instructions}], "return_type": result_name, "return_concepts": [concept_type(result).map(|value| value.0).unwrap_or("unknown")]})
}

fn lower_expression(
    expression: &Expr,
    function: &FunctionInfo<'_>,
    index: &Index<'_>,
    by_id: &BTreeMap<String, usize>,
    instructions: &mut Vec<Value>,
    values: &BTreeMap<String, String>,
    value_types: &BTreeMap<String, String>,
    expected: Option<&str>,
    calls: &mut Vec<String>,
) -> Option<(String, String)> {
    match expression {
        Expr::Path(path) if path.path.segments.len() == 1 => {
            let name = path.path.segments.last()?.ident.to_string();
            let value = values.get(&name)?.clone();
            let value_type = value_types.get(&name)?.clone();
            if expected.is_some_and(|wanted| wanted != value_type) {
                return None;
            }
            Some((value, value_type))
        }
        Expr::Lit(literal) => {
            let value_type = expected?.to_owned();
            if primitive_type_name(&value_type).is_none() {
                return None;
            }
            let id = format!("n{}", instructions.len() + 1);
            let value = scalar_literal(&literal.lit, &value_type)?;
            instructions.push(json!({"id": id, "opcode": "constant", "type": value_type, "value_kind": value_kind_name(&value_type), "value": {"type": value_type, "value_kind": value_kind_name(&value_type), "constant": value}, "results": [id]}));
            Some((id, value_type))
        }
        Expr::Unary(value) if matches!(value.op, syn::UnOp::Not(_)) => {
            let (operand, operand_type) = lower_expression(
                &value.expr,
                function,
                index,
                by_id,
                instructions,
                values,
                value_types,
                Some("bool"),
                calls,
            )?;
            if operand_type != "bool" || expected.is_some_and(|wanted| wanted != "bool") {
                return None;
            }
            let id = format!("n{}", instructions.len() + 1);
            instructions.push(json!({"id": id, "opcode": "primitive", "type": "bool", "value_kind": "boolean", "operator": "!", "arithmetic_mode": "boolean", "operands": [operand], "results": [id]}));
            Some((id, "bool".to_owned()))
        }
        Expr::Call(call) => {
            let target = resolve_call(&function.module, &call.func, index, by_id)?;
            let target_info = index.functions.iter().find(|item| item.id == target)?;
            if call.args.len() != target_info.function.sig.inputs.len() {
                return None;
            }
            let mut operands = Vec::new();
            let mut bindings = Vec::new();
            for (position, argument) in call.args.iter().enumerate() {
                let ty = argument_type(&target_info.function.sig.inputs[position])?;
                let (_, expected_type) = concept_type(ty)?;
                let (operand, operand_type) = lower_expression(
                    argument,
                    function,
                    index,
                    by_id,
                    instructions,
                    values,
                    value_types,
                    Some(expected_type.as_str()),
                    calls,
                )?;
                if operand_type != expected_type {
                    return None;
                }
                operands.push(operand.clone());
                bindings.push(json!({"formal": format!("arg{position}"), "actual": operand}));
            }
            let result = result_type(target_info.function)?;
            let (_, result_name) = concept_type(result)?;
            if expected.is_some_and(|wanted| wanted != result_name) {
                return None;
            }
            let id = format!("n{}", instructions.len() + 1);
            instructions.push(json!({"id": id, "opcode": "call", "type": result_name, "value_kind": value_kind(result), "operands": operands, "results": [id], "call": {"targets": [target], "bindings": bindings}}));
            calls.push(target);
            Some((id, result_name))
        }
        Expr::MethodCall(call)
            if matches!(
                call.method.to_string().as_str(),
                "wrapping_add" | "wrapping_mul"
            ) && call.args.len() == 1 =>
        {
            let Expr::Path(receiver) = call.receiver.as_ref() else {
                return None;
            };
            if receiver.path.segments.len() != 1 {
                return None;
            }
            let receiver_name = receiver.path.segments.last()?.ident.to_string();
            let receiver_type = value_types.get(&receiver_name)?.clone();
            if !matches!(receiver_type.as_str(), "i32" | "i64")
                || expected.is_some_and(|wanted| wanted != receiver_type)
            {
                return None;
            }
            let (argument, argument_type) = lower_expression(
                &call.args[0],
                function,
                index,
                by_id,
                instructions,
                values,
                value_types,
                Some(&receiver_type),
                calls,
            )?;
            if argument_type != receiver_type {
                return None;
            }
            let id = format!("n{}", instructions.len() + 1);
            let operator = if call.method == "wrapping_add" {
                "+"
            } else {
                "*"
            };
            instructions.push(json!({"id": id, "opcode": "primitive", "type": receiver_type, "value_kind": "numeric", "operator": operator, "arithmetic_mode": "wrapping", "operands": [values.get(&receiver_name)?, argument], "results": [id]}));
            Some((id, receiver_type))
        }
        _ => None,
    }
}

fn resolve_call(
    module: &[String],
    path: &syn::Expr,
    index: &Index<'_>,
    by_id: &BTreeMap<String, usize>,
) -> Option<String> {
    let Expr::Path(value) = path else { return None };
    resolve_path(
        module,
        &value
            .path
            .segments
            .iter()
            .map(|segment| segment.ident.to_string())
            .collect::<Vec<_>>(),
        index,
        by_id,
    )
}
fn resolve_path(
    module: &[String],
    path: &[String],
    index: &Index<'_>,
    by_id: &BTreeMap<String, usize>,
) -> Option<String> {
    resolve_path_bounded(module, path, index, by_id, 0)
}

fn resolve_path_bounded(
    module: &[String],
    path: &[String],
    index: &Index<'_>,
    by_id: &BTreeMap<String, usize>,
    depth: usize,
) -> Option<String> {
    if depth >= 64 {
        return None;
    }
    if path.is_empty() {
        return None;
    }
    let mut candidates = Vec::new();
    let mut target = path.to_vec();
    if target[0] == "crate" {
        target.remove(0);
        candidates.push(target.clone());
    } else if target[0] == "self" {
        target.remove(0);
        let mut candidate = module.to_vec();
        candidate.extend(target.clone());
        candidates.push(candidate);
    } else if target[0] == "super" {
        target.remove(0);
        let mut candidate = module.to_vec();
        let _ = candidate.pop();
        candidate.extend(target.clone());
        candidates.push(candidate);
    } else {
        let mut candidate = module.to_vec();
        candidate.extend(target.clone());
        candidates.push(candidate);
        candidates.push(target.clone());
    }
    for candidate in candidates {
        let name = candidate.last()?.clone();
        let candidate_module = &candidate[..candidate.len() - 1];
        let id = function_id(&index.artifact, candidate_module, &name);
        if by_id.contains_key(&id) {
            return Some(id);
        }
        for alias in &index.aliases {
            if alias.module == candidate_module && alias.name == name && !alias.unresolved {
                return resolve_path_bounded(&alias.module, &alias.target, index, by_id, depth + 1);
            }
        }
    }
    None
}
fn function_id(path: &str, module: &[String], name: &str) -> String {
    if module.is_empty() {
        format!("{path}/{name}")
    } else {
        format!("{path}/{}/{}", module_prefix(module), name)
    }
}
fn module_prefix(module: &[String]) -> String {
    if module.is_empty() {
        "".to_owned()
    } else {
        module.join("::")
    }
}

fn likely_crate_root(path: &str) -> bool {
    let basename = path.rsplit('/').next().unwrap_or(path);
    basename == "lib.rs"
        || basename == "main.rs"
        || path.split('/').any(|component| component == "bin")
}

fn rust_signature(function: &syn::ItemFn) -> Option<String> {
    let params = function
        .sig
        .inputs
        .iter()
        .map(argument_type)
        .map(|value| value.and_then(rust_type_signature))
        .collect::<Option<Vec<_>>>()?
        .join(",");
    let result = match &function.sig.output {
        ReturnType::Default => "()".to_owned(),
        ReturnType::Type(_, ty) => rust_type_signature(ty)?,
    };
    Some(format!("({params})->{result}"))
}
fn rust_type_signature(ty: &Type) -> Option<String> {
    match ty {
        Type::Path(path) if path.qself.is_none() => Some(
            path.path
                .segments
                .iter()
                .map(|segment| segment.ident.to_string())
                .collect::<Vec<_>>()
                .join("::"),
        ),
        Type::Tuple(tuple) => Some(format!(
            "({})",
            tuple
                .elems
                .iter()
                .map(rust_type_signature)
                .collect::<Option<Vec<_>>>()?
                .join(",")
        )),
        Type::Paren(paren) => rust_type_signature(&paren.elem),
        Type::Never(_) => Some("!".to_owned()),
        _ => None,
    }
}
fn scalar_literal(literal: &syn::Lit, expected: &str) -> Option<String> {
    match literal {
        syn::Lit::Bool(value) if expected == "bool" => Some(value.value.to_string()),
        syn::Lit::Int(value) => {
            if !value.suffix().is_empty() && value.suffix() != expected {
                return None;
            }
            let number = value.base10_parse::<i64>().ok()?;
            match expected {
                "i32" if i32::try_from(number).is_ok() => Some(number.to_string()),
                "i64" => Some(number.to_string()),
                _ => None,
            }
        }
        _ => None,
    }
}
fn local_name(local: &syn::Local) -> Option<String> {
    match &local.pat {
        syn::Pat::Ident(value) if value.by_ref.is_none() && value.subpat.is_none() => {
            Some(value.ident.to_string())
        }
        _ => None,
    }
}
fn argument_type(argument: &FnArg) -> Option<&Type> {
    match argument {
        FnArg::Typed(argument) => Some(&argument.ty),
        FnArg::Receiver(_) => None,
    }
}
fn argument_name(argument: &FnArg) -> Option<String> {
    match argument {
        FnArg::Typed(argument) => match argument.pat.as_ref() {
            syn::Pat::Ident(value) if value.by_ref.is_none() && value.subpat.is_none() => {
                Some(value.ident.to_string())
            }
            _ => None,
        },
        FnArg::Receiver(_) => None,
    }
}
fn result_type(function: &syn::ItemFn) -> Option<&Type> {
    match &function.sig.output {
        ReturnType::Type(_, value) => Some(value),
        ReturnType::Default => None,
    }
}
fn concept_type(ty: &Type) -> Option<(&'static str, String)> {
    let Type::Path(path) = ty else { return None };
    if path.path.segments.len() != 1
        || !matches!(path.path.segments[0].arguments, syn::PathArguments::None)
    {
        return None;
    }
    match path.path.segments[0].ident.to_string().as_str() {
        "i32" | "i64" => Some(("number", ty_name(ty))),
        "bool" => Some(("boolean", ty_name(ty))),
        _ => None,
    }
}
fn ty_name(ty: &Type) -> String {
    match ty {
        Type::Path(path) => path
            .path
            .segments
            .last()
            .map(|segment| segment.ident.to_string())
            .unwrap_or_else(|| "unknown".to_owned()),
        _ => "unknown".to_owned(),
    }
}
fn primitive_type_name(name: &str) -> Option<&'static str> {
    match name {
        "i32" => Some("i32"),
        "i64" => Some("i64"),
        "bool" => Some("bool"),
        _ => None,
    }
}
fn value_kind(ty: &Type) -> &'static str {
    match concept_type(ty).map(|value| value.0) {
        Some("number") => "numeric",
        Some("boolean") => "boolean",
        _ => "unknown",
    }
}
fn value_kind_name(name: &str) -> &'static str {
    match name {
        "i32" | "i64" => "numeric",
        "bool" => "boolean",
        _ => "unknown",
    }
}

fn item_cfg(item: &Item) -> CfgState {
    let attrs = match item {
        Item::Const(v) => &v.attrs,
        Item::Enum(v) => &v.attrs,
        Item::ExternCrate(v) => &v.attrs,
        Item::Fn(v) => &v.attrs,
        Item::ForeignMod(v) => &v.attrs,
        Item::Impl(v) => &v.attrs,
        Item::Macro(v) => &v.attrs,
        Item::Mod(v) => &v.attrs,
        Item::Static(v) => &v.attrs,
        Item::Struct(v) => &v.attrs,
        Item::Trait(v) => &v.attrs,
        Item::TraitAlias(v) => &v.attrs,
        Item::Type(v) => &v.attrs,
        Item::Union(v) => &v.attrs,
        Item::Use(v) => &v.attrs,
        Item::Verbatim(_) => return CfgState::Unknown,
        _ => return CfgState::Unknown,
    };
    attrs_cfg(attrs)
}
fn attrs_cfg(attrs: &[syn::Attribute]) -> CfgState {
    let mut state = CfgState::Enabled;
    for attr in attrs {
        if attr.path.is_ident("cfg_attr") {
            return CfgState::Unknown;
        }
        if !attr.path.is_ident("cfg") {
            continue;
        }
        let Ok(Meta::List(list)) = attr.parse_meta() else {
            return CfgState::Unknown;
        };
        let value = if list.path.is_ident("cfg") && list.nested.len() == 1 {
            cfg_nested(list.nested.first().unwrap())
        } else {
            CfgState::Unknown
        };
        state = cfg_all(state, value);
    }
    state
}
fn cfg_all(left: CfgState, right: CfgState) -> CfgState {
    match (left, right) {
        (CfgState::Disabled, _) | (_, CfgState::Disabled) => CfgState::Disabled,
        (CfgState::Unknown, _) | (_, CfgState::Unknown) => CfgState::Unknown,
        _ => CfgState::Enabled,
    }
}
fn cfg_meta_list(
    path: &syn::Path,
    items: &syn::punctuated::Punctuated<syn::NestedMeta, syn::token::Comma>,
) -> CfgState {
    if path.is_ident("any") {
        return cfg_any(items);
    }
    if path.is_ident("all") {
        return cfg_all_list(items);
    }
    if path.is_ident("not") {
        if items.len() != 1 {
            return CfgState::Unknown;
        }
        return match cfg_nested(items.first().unwrap()) {
            CfgState::Enabled => CfgState::Disabled,
            CfgState::Disabled => CfgState::Enabled,
            CfgState::Unknown => CfgState::Unknown,
        };
    }
    CfgState::Unknown
}
fn cfg_nested(item: &syn::NestedMeta) -> CfgState {
    match item {
        syn::NestedMeta::Meta(Meta::List(list)) => cfg_meta_list(&list.path, &list.nested),
        _ => CfgState::Unknown,
    }
}
fn cfg_any(items: &syn::punctuated::Punctuated<syn::NestedMeta, syn::token::Comma>) -> CfgState {
    if items.is_empty() {
        return CfgState::Disabled;
    }
    let mut unknown = false;
    for item in items {
        match cfg_nested(item) {
            CfgState::Enabled => return CfgState::Enabled,
            CfgState::Unknown => unknown = true,
            CfgState::Disabled => {}
        }
    }
    if unknown {
        CfgState::Unknown
    } else {
        CfgState::Disabled
    }
}
fn cfg_all_list(
    items: &syn::punctuated::Punctuated<syn::NestedMeta, syn::token::Comma>,
) -> CfgState {
    if items.is_empty() {
        return CfgState::Enabled;
    }
    let mut unknown = false;
    for item in items {
        match cfg_nested(item) {
            CfgState::Disabled => return CfgState::Disabled,
            CfgState::Unknown => unknown = true,
            CfgState::Enabled => {}
        }
    }
    if unknown {
        CfgState::Unknown
    } else {
        CfgState::Enabled
    }
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
