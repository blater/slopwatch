use syn::{FnArg, ReturnType, Type};

pub(super) fn rust_signature(function: &syn::ItemFn) -> Option<String> {
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
pub(super) fn scalar_literal(literal: &syn::Lit, expected: &str) -> Option<String> {
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
pub(super) fn local_name(local: &syn::Local) -> Option<String> {
    match &local.pat {
        syn::Pat::Ident(value) if value.by_ref.is_none() && value.subpat.is_none() => {
            Some(value.ident.to_string())
        }
        _ => None,
    }
}
pub(super) fn argument_type(argument: &FnArg) -> Option<&Type> {
    match argument {
        FnArg::Typed(argument) => Some(&argument.ty),
        FnArg::Receiver(_) => None,
    }
}
pub(super) fn argument_name(argument: &FnArg) -> Option<String> {
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
pub(super) fn result_type(function: &syn::ItemFn) -> Option<&Type> {
    match &function.sig.output {
        ReturnType::Type(_, value) => Some(value),
        ReturnType::Default => None,
    }
}
pub(super) fn concept_type(ty: &Type) -> Option<(&'static str, String)> {
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
pub(super) fn primitive_type_name(name: &str) -> Option<&'static str> {
    match name {
        "i32" => Some("i32"),
        "i64" => Some("i64"),
        "bool" => Some("bool"),
        _ => None,
    }
}
pub(super) fn value_kind(ty: &Type) -> &'static str {
    match concept_type(ty).map(|value| value.0) {
        Some("number") => "numeric",
        Some("boolean") => "boolean",
        _ => "unknown",
    }
}
pub(super) fn value_kind_name(name: &str) -> &'static str {
    match name {
        "i32" | "i64" => "numeric",
        "bool" => "boolean",
        _ => "unknown",
    }
}
