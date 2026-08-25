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

pub(crate) fn collect(
    items: &[syn::Item],
    path: &str,
    operations: &mut Vec<PublicOperation>,
    exposures: &mut Vec<RepresentationExposure>,
) {
    for item in items {
        match item {
            syn::Item::Fn(function) if matches!(function.vis, syn::Visibility::Public(_)) => {
                operations.push(function_operation(function, path))
            }
            syn::Item::Impl(value) => collect_impl(value, path, operations),
            syn::Item::Trait(value) if matches!(value.vis, syn::Visibility::Public(_)) => {
                collect_trait(value, path, operations)
            }
            syn::Item::Struct(value) => collect_struct(value, path, exposures),
            syn::Item::Mod(module) => collect_module(module, path, operations, exposures),
            _ => {}
        }
    }
}

fn collect_impl(value: &syn::ItemImpl, path: &str, operations: &mut Vec<PublicOperation>) {
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
                operations.push(method_operation(method, path, &owner));
            }
        }
    }
}

fn collect_trait(value: &syn::ItemTrait, path: &str, operations: &mut Vec<PublicOperation>) {
    let owner = value.ident.to_string();
    for member in &value.items {
        if let syn::TraitItem::Method(method) = member {
            operations.push(trait_method_operation(method, path, &owner));
        }
    }
}

fn collect_struct(
    value: &syn::ItemStruct,
    path: &str,
    exposures: &mut Vec<RepresentationExposure>,
) {
    for field in &value.fields {
        if !matches!(field.vis, syn::Visibility::Public(_)) {
            continue;
        }
        let name = field
            .ident
            .as_ref()
            .map(ToString::to_string)
            .unwrap_or_else(|| "tuple".to_owned());
        exposures.push(RepresentationExposure {
            stable_id: format!("{path}:{}:{name}", value.ident),
            kind: "public-mutable-field".to_owned(),
            entity: format!("{}.{}", value.ident, name),
            location: location(path, field.span()),
            evidence: "public Rust struct field exposes representation".to_owned(),
            confidence: "exact".to_owned(),
        });
    }
}

fn collect_module(
    module: &syn::ItemMod,
    path: &str,
    operations: &mut Vec<PublicOperation>,
    exposures: &mut Vec<RepresentationExposure>,
) {
    if let Some((_, nested)) = &module.content {
        collect(nested, path, operations, exposures);
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

#[cfg(test)]
mod tests {
    use super::collect;
    use crate::model::{PublicOperation, RepresentationExposure};

    fn operations(source: &str) -> Vec<PublicOperation> {
        let syntax = syn::parse_file(source).expect("source parses");
        let mut operations = Vec::new();
        let mut exposures: Vec<RepresentationExposure> = Vec::new();
        collect(
            &syntax.items,
            "participant.rs",
            &mut operations,
            &mut exposures,
        );
        operations
    }

    #[test]
    fn public_trait_methods_are_public_operations() {
        let operations_out = operations(
            "pub trait Participant { fn commit(&self, transaction_id: u64) -> StatusCode; }",
        );
        assert_eq!(operations_out.len(), 1);
        assert_eq!(operations_out[0].name, "commit");
        assert_eq!(operations_out[0].parameters.len(), 1);
        assert_eq!(operations_out[0].results.len(), 1);
    }

    #[test]
    fn private_trait_methods_are_not_public_operations() {
        let operations_out = operations("trait Participant { fn commit(&self); }");
        assert!(operations_out.is_empty());
    }

    #[test]
    fn one_walk_collects_nested_operations_and_exposures() {
        let syntax = syn::parse_file(
            "mod nested { pub struct Participant { pub state: u64 } pub fn commit() {} }",
        )
        .expect("module parses");
        let mut operations = Vec::new();
        let mut exposures = Vec::new();
        collect(
            &syntax.items,
            "participant.rs",
            &mut operations,
            &mut exposures,
        );

        assert_eq!(operations.len(), 1);
        assert_eq!(operations[0].name, "commit");
        assert_eq!(exposures.len(), 1);
        assert_eq!(exposures[0].entity, "Participant.state");
    }
}
