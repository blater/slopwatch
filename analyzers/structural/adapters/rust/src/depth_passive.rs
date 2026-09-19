use serde_json::{json, Value};
use std::collections::{BTreeMap, BTreeSet};
use syn::{Expr, FnArg, Item, ReturnType, Stmt, Type, Visibility};

struct PassiveCarrierCounts {
    fields: usize,
    getters: usize,
    mutators: usize,
    constructors: usize,
}

pub(super) fn passive_result_carrier(path: &str, syntax: &syn::File) -> Option<Value> {
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
                    if !passive_public_method(
                        method,
                        &carrier_name,
                        &field_types,
                        &constants,
                        &mut counts,
                        &mut getter_fields,
                    ) {
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

fn passive_public_method(
    method: &syn::ImplItemMethod,
    carrier: &str,
    fields: &BTreeMap<String, String>,
    constants: &BTreeMap<String, String>,
    counts: &mut PassiveCarrierCounts,
    getter_fields: &mut BTreeSet<String>,
) -> bool {
    if !passive_method_shape(method) {
        return false;
    }
    let receiver = match method.sig.inputs.first() {
        Some(FnArg::Receiver(receiver)) => receiver,
        _ => {
            if !passive_constructor(method, carrier, fields, constants) {
                return false;
            }
            counts.constructors += 1;
            return true;
        }
    };
    if receiver.reference.is_none() {
        return false;
    }
    if receiver.mutability.is_none()
        && method.sig.inputs.len() == 1
        && passive_getter(method, fields)
            .map(|field| getter_fields.insert(field))
            .unwrap_or(false)
    {
        counts.getters += 1;
        return true;
    }
    if receiver.mutability.is_some() && passive_mutator(method, fields, constants) {
        counts.mutators += 1;
        return true;
    }
    false
}

pub(super) fn passive_value_enum(path: &str, syntax: &syn::File) -> Option<Value> {
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

pub(super) fn passive_value_object(path: &str, syntax: &syn::File) -> Option<Value> {
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
