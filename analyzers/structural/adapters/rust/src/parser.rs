use crate::design::{collect_functions, declare_types, normalize_types};
use crate::location::location;
use crate::model::Program;
use crate::surface;
use std::fs;
use std::path::{Component, Path, PathBuf};
use syn::spanned::Spanned;

/* fn shape(ty: &syn::Type) -> TypeShape {
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
} */

/* pub(crate) fn operations(items: &[syn::Item], path: &str, output: &mut Vec<PublicOperation>) {
    for item in items {
        match item {
            syn::Item::Fn(function) if matches!(function.vis, syn::Visibility::Public(_)) => {
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
                output.push(operation);
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
                        if !matches!(method.vis, syn::Visibility::Public(_)) {
                            continue;
                        }
                        let mut operation = PublicOperation {
                            stable_id: format!("{path}:{owner}:{}", method.sig.ident),
                            name: method.sig.ident.to_string(),
                            owner_type: owner.clone(),
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
                        output.push(operation);
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
} */

/* pub(crate) fn exposures(items: &[syn::Item], path: &str, output: &mut Vec<RepresentationExposure>) {
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
} */

pub(crate) fn canonical_source(
    workspace: &Path,
    requested: &str,
) -> Result<(PathBuf, String), String> {
    let relative = Path::new(requested);
    if requested.contains('\\')
        || requested
            .split('/')
            .any(|part| part.is_empty() || part == "." || part == "..")
        || relative.is_absolute()
        || relative.components().any(|part| {
            matches!(
                part,
                Component::ParentDir
                    | Component::CurDir
                    | Component::RootDir
                    | Component::Prefix(_)
            )
        })
        || !requested.ends_with(".rs")
    {
        return Err(format!("non-canonical Rust source path: {requested}"));
    }
    let mut current = workspace.to_path_buf();
    for component in relative.components() {
        current.push(component);
        let metadata = fs::symlink_metadata(&current).map_err(|error| error.to_string())?;
        if metadata.file_type().is_symlink() {
            return Err(format!("symlinked Rust source is not allowed: {requested}"));
        }
    }
    let candidate = workspace.join(relative);
    let absolute = candidate
        .canonicalize()
        .map_err(|error| error.to_string())?;
    if !absolute.starts_with(workspace) {
        return Err(format!("Rust source escapes workspace: {requested}"));
    }
    Ok((absolute, requested.replace('\\', "/")))
}

pub fn parse_program(
    workspace: &Path,
    paths: &[String],
    include_tests: bool,
) -> Result<Program, String> {
    let workspace = workspace
        .canonicalize()
        .map_err(|error| error.to_string())?;
    let mut parsed = Vec::with_capacity(paths.len());
    for requested in paths {
        let (absolute, relative) = canonical_source(&workspace, requested)?;
        let source = fs::read_to_string(absolute).map_err(|error| error.to_string())?;
        let syntax = syn::parse_file(&source)
            .map_err(|error| format!("failed to parse {relative}: {error}"))?;
        parsed.push((relative, syntax));
    }
    let mut program = Program {
        files: paths.to_vec(),
        ..Program::default()
    };
    for (path, syntax) in &parsed {
        declare_types(&syntax.items, path, &mut program.types, include_tests);
    }
    for (path, syntax) in &parsed {
        collect_functions(
            &syntax.items,
            path,
            &mut program.types,
            &mut program.functions,
            include_tests,
        );
        surface::operations(&syntax.items, path, &mut program.public_operations);
        surface::exposures(&syntax.items, path, &mut program.representation);
    }
    normalize_types(&mut program.types);
    program.functions.sort_by_key(|item| {
        (
            item.location.path.clone(),
            item.location.line,
            item.location.column,
        )
    });
    program.types.sort_by_key(|item| {
        (
            item.location.path.clone(),
            item.location.line,
            item.location.column,
        )
    });
    program.public_operations.sort_by_key(|item| {
        (
            item.location.path.clone(),
            item.location.line,
            item.location.column,
        )
    });
    Ok(program)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::{expression_kind, statement_kind};

    #[test]
    fn builds_control_and_design_facts_from_real_syntax() {
        let syntax = syn::parse_file(
            r#"
            struct Peer { value: i32 }
            struct Service { state: i32, peer: Peer }
            impl Service {
                fn run(&self, other: Peer, ok: bool) -> i32 {
                    if ok && self.state > 0 { return other.value; }
                    match self.state { 0 => 0, _ => 1 }
                }
            }
            fn launch() { let callback = || { if true {} }; callback(); }
            #[cfg(test)]
            mod tests { #[test] fn inline_unit_test() { if true {} } }
            "#,
        )
        .expect("fixture parses");
        let mut program = Program {
            files: vec!["sample.rs".to_owned()],
            ..Program::default()
        };
        declare_types(&syntax.items, "sample.rs", &mut program.types, false);
        collect_functions(
            &syntax.items,
            "sample.rs",
            &mut program.types,
            &mut program.functions,
            false,
        );
        normalize_types(&mut program.types);

        assert_eq!(program.functions.len(), 2);
        let run = program
            .functions
            .iter()
            .find(|item| item.name == "run")
            .unwrap();
        assert_eq!(run.body[0].kind, statement_kind::IF);
        assert_eq!(
            run.body[0].condition.as_ref().unwrap().kind,
            expression_kind::AND
        );
        assert_eq!(run.body[1].kind, statement_kind::SWITCH);
        let service = program
            .types
            .iter()
            .find(|item| item.name == "Service")
            .unwrap();
        assert_eq!(service.methods.len(), 1);
        assert_eq!(service.method_fields["run"], vec!["state"]);
        assert_eq!(service.foreign_fields, vec!["other.value"]);
        assert!(service.foreign_types.contains(&"Peer".to_owned()));

        let mut test_functions = Vec::new();
        collect_functions(
            &syntax.items,
            "sample.rs",
            &mut program.types,
            &mut test_functions,
            true,
        );
        assert!(test_functions
            .iter()
            .any(|item| item.name == "inline_unit_test"));
    }
}
