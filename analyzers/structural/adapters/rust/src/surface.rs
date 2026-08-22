use crate::location::location;
use crate::model::{PublicOperation, RepresentationExposure, TypeShape};
use syn::spanned::Spanned;

fn shape(ty: &syn::Type) -> TypeShape {
    let name = match ty {
        syn::Type::Path(value) => value
            .path
            .segments
            .last()
            .map(|item| item.ident.to_string())
            .unwrap_or_else(|| "type".to_owned()),
        syn::Type::Reference(_) => "reference".to_owned(),
        syn::Type::Array(_) => "array".to_owned(),
        syn::Type::Slice(_) => "slice".to_owned(),
        syn::Type::Tuple(_) => "tuple".to_owned(),
        _ => "type".to_owned(),
    };
    let mut result = TypeShape {
        kind: "type".to_owned(),
        name,
        complexity: 1,
        ..TypeShape::default()
    };
    match ty {
        syn::Type::Reference(value) => result.children.push(shape(&value.elem)),
        syn::Type::Array(value) => result.children.push(shape(&value.elem)),
        syn::Type::Slice(value) => result.children.push(shape(&value.elem)),
        syn::Type::Tuple(value) => result.children.extend(value.elems.iter().map(shape)),
        syn::Type::Path(value) => {
            if let Some(segment) = value.path.segments.last() {
                if let syn::PathArguments::AngleBracketed(arguments) = &segment.arguments {
                    for argument in &arguments.args {
                        if let syn::GenericArgument::Type(value) = argument {
                            result.children.push(shape(value));
                        }
                    }
                }
            }
        }
        _ => {}
    }
    result.complexity = (1 + result
        .children
        .iter()
        .map(|child| child.complexity)
        .sum::<usize>())
    .min(32);
    result.stable_id = format!("type:{}:{}", result.name, result.complexity);
    result
}

pub(crate) fn operations(items: &[syn::Item], path: &str, output: &mut Vec<PublicOperation>) {
    for item in items {
        match item {
            syn::Item::Fn(function) if matches!(function.vis, syn::Visibility::Public(_)) => {
                output.push(function_operation(function, path))
            }
            syn::Item::Impl(value) => {
                let owner = match value.self_ty.as_ref() {
                    syn::Type::Path(value) => value
                        .path
                        .segments
                        .last()
                        .map(|item| item.ident.to_string())
                        .unwrap_or_else(|| "type".to_owned()),
                    _ => "type".to_owned(),
                };
                for member in &value.items {
                    if let syn::ImplItem::Method(method) = member {
                        if matches!(method.vis, syn::Visibility::Public(_)) {
                            output.push(method_operation(method, path, &owner));
                        }
                    }
                }
            }
            syn::Item::Trait(value) if matches!(value.vis, syn::Visibility::Public(_)) => {
                for member in &value.items {
                    if let syn::TraitItem::Method(method) = member {
                        output.push(trait_method_operation(
                            method,
                            path,
                            &value.ident.to_string(),
                        ));
                    }
                }
            }
            syn::Item::Mod(module) => {
                if let Some((_, nested)) = &module.content {
                    operations(nested, path, output);
                }
            }
            _ => {}
        }
    }
}

fn function_operation(function: &syn::ItemFn, path: &str) -> PublicOperation {
    let mut operation = PublicOperation {
        stable_id: format!("{path}::{}", function.sig.ident),
        name: function.sig.ident.to_string(),
        location: location(path, function.span()),
        ..PublicOperation::default()
    };
    for input in &function.sig.inputs {
        if let syn::FnArg::Typed(value) = input {
            operation.parameters.push(shape(&value.ty));
        }
    }
    if let syn::ReturnType::Type(_, value) = &function.sig.output {
        operation.results.push(shape(value));
        operation.emits_output = true;
    }
    operation
}

fn method_operation(method: &syn::ImplItemMethod, path: &str, owner: &str) -> PublicOperation {
    let mut operation = PublicOperation {
        stable_id: format!("{path}:{owner}:{}", method.sig.ident),
        name: method.sig.ident.to_string(),
        owner_type: owner.to_owned(),
        location: location(path, method.span()),
        ..PublicOperation::default()
    };
    for input in &method.sig.inputs {
        if let syn::FnArg::Typed(value) = input {
            operation.parameters.push(shape(&value.ty));
        }
    }
    if let syn::ReturnType::Type(_, value) = &method.sig.output {
        operation.results.push(shape(value));
        operation.emits_output = true;
    }
    operation
}

fn trait_method_operation(
    method: &syn::TraitItemMethod,
    path: &str,
    owner: &str,
) -> PublicOperation {
    let mut operation = PublicOperation {
        stable_id: format!("{path}:{owner}:{}", method.sig.ident),
        name: method.sig.ident.to_string(),
        owner_type: owner.to_owned(),
        location: location(path, method.span()),
        ..PublicOperation::default()
    };
    for input in &method.sig.inputs {
        if let syn::FnArg::Typed(value) = input {
            operation.parameters.push(shape(&value.ty));
        }
    }
    if let syn::ReturnType::Type(_, value) = &method.sig.output {
        operation.results.push(shape(value));
        operation.emits_output = true;
    }
    operation
}

pub(crate) fn exposures(items: &[syn::Item], path: &str, output: &mut Vec<RepresentationExposure>) {
    for item in items {
        match item {
            syn::Item::Struct(value) => {
                for field in &value.fields {
                    if matches!(field.vis, syn::Visibility::Public(_)) {
                        let name = field
                            .ident
                            .as_ref()
                            .map(ToString::to_string)
                            .unwrap_or_else(|| "tuple".to_owned());
                        output.push(RepresentationExposure {
                            stable_id: format!("{path}:{}:{name}", value.ident),
                            kind: "public-mutable-field".to_owned(),
                            entity: format!("{}.{}", value.ident, name),
                            location: location(path, field.span()),
                            evidence: "public Rust struct field exposes representation".to_owned(),
                            confidence: "exact".to_owned(),
                        });
                    }
                }
            }
            syn::Item::Mod(module) => {
                if let Some((_, nested)) = &module.content {
                    exposures(nested, path, output);
                }
            }
            _ => {}
        }
    }
}

#[cfg(test)]
mod tests {
    use super::operations;
    use crate::model::PublicOperation;

    #[test]
    fn public_trait_methods_are_public_operations() {
        let syntax = syn::parse_file(
            "pub trait Participant { fn commit(&self, transaction_id: u64) -> StatusCode; }",
        )
        .expect("trait parses");
        let mut operations_out: Vec<PublicOperation> = Vec::new();
        operations(&syntax.items, "participant.rs", &mut operations_out);
        assert_eq!(operations_out.len(), 1);
        assert_eq!(operations_out[0].name, "commit");
        assert_eq!(operations_out[0].parameters.len(), 1);
        assert_eq!(operations_out[0].results.len(), 1);
    }

    #[test]
    fn private_trait_methods_are_not_public_operations() {
        let syntax =
            syn::parse_file("trait Participant { fn commit(&self); }").expect("trait parses");
        let mut operations_out: Vec<PublicOperation> = Vec::new();
        operations(&syntax.items, "participant.rs", &mut operations_out);
        assert!(operations_out.is_empty());
    }
}
