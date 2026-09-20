use crate::design::{collect_functions, declare_types, normalize_types, TypeOwnerIndex};
use crate::model::Program;
use crate::surface;
use std::collections::HashSet;
use std::fs;
use std::path::{Component, Path, PathBuf};

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
    let candidate = workspace.join(relative);
    let metadata = fs::metadata(&candidate).map_err(|error| error.to_string())?;
    if !metadata.is_file() {
        return Err(format!("Rust source is not a regular file: {requested}"));
    }
    let resolved = candidate
        .canonicalize()
        .map_err(|error| error.to_string())?;
    if !resolved.starts_with(workspace) {
        return Err(format!("Rust source escapes workspace: {requested}"));
    }
    Ok((candidate, requested.replace('\\', "/")))
}

pub fn parse_program(
    workspace: &Path,
    paths: &[String],
    include_tests: bool,
    depth: bool,
) -> Result<Program, String> {
    parse_program_progress(workspace, paths, include_tests, depth, false)
}

pub fn parse_program_progress(workspace: &Path, paths: &[String], include_tests: bool, depth: bool, stream: bool) -> Result<Program, String> {
    let workspace = workspace
        .canonicalize()
        .map_err(|error| error.to_string())?;
    let mut parsed = Vec::with_capacity(paths.len());
    let mut valid_files = Vec::with_capacity(paths.len());
    let mut failures = Vec::new();
    let mut seen = HashSet::with_capacity(paths.len());
    for (position, requested) in paths.iter().enumerate() {
        crate::stream::progress(stream, "parsing", position, paths.len())?;
        if !seen.insert(requested) {
            return Err(format!("duplicate Rust source path: {requested}"));
        }
        let (absolute, relative) = match canonical_source(&workspace, requested) {
            Ok(value) => value,
            Err(error) => {
                failures.push(crate::model::FileFailure {
                    path: requested.clone(),
                    code: source_failure_code(requested, &error),
                    diagnostic: format!("{requested}: {error}"),
                });
                continue;
            }
        };
        let source = match fs::read_to_string(&absolute) {
            Ok(value) => value,
            Err(error) => {
                failures.push(crate::model::FileFailure {
                    path: relative,
                    code: "SOURCE_READ_ERROR".to_owned(),
                    diagnostic: format!("{requested}: {error}"),
                });
                continue;
            }
        };
        match syn::parse_file(&source) {
            Ok(syntax) => {
                valid_files.push(relative.clone());
                parsed.push((relative, syntax));
            }
            Err(error) => {
                let start = error.span().start();
                failures.push(crate::model::FileFailure {
                    path: relative.clone(),
                    code: "SYNTAX_ERROR".to_owned(),
                    diagnostic: format!(
                        "{}:{}:{}: {}",
                        relative,
                        start.line,
                        start.column + 1,
                        error
                    ),
                });
            }
        }
    }
    crate::stream::progress(stream, "parsing", paths.len(), paths.len())?;
    let mut program = Program {
        files: valid_files,
        failures,
        ..Program::default()
    };
    if !program.failures.is_empty() {
        for path in &program.files {
            let components = [
                "cyclomatic_class_complexity",
                "god_class",
                "coupling_between_objects",
            ]
            .into_iter()
            .map(|component| {
                (
                    component.to_owned(),
                    "syntax errors in the requested Rust context prevent trustworthy cross-file type evidence"
                        .to_owned(),
                )
            })
            .collect();
            program.unavailable.insert(path.clone(), components);
        }
    }
    for (position, (path, syntax)) in parsed.iter().enumerate() {
        crate::stream::progress(stream, "types", position, parsed.len())?;
        declare_types(&syntax.items, path, &mut program.types, include_tests);
    }
    let type_owners = TypeOwnerIndex::new(&program.types);
    for (position, (path, syntax)) in parsed.iter().enumerate() {
        crate::stream::progress(stream, "syntax", position, parsed.len())?;
        collect_functions(
            &syntax.items,
            path,
            &mut program.types,
            &type_owners,
            &mut program.functions,
            include_tests,
        );
        surface::collect(
            &syntax.items,
            path,
            &mut program.public_operations,
            &mut program.representation,
        );
    }
    normalize_types(&mut program.types);
    program.functions.sort_by(|left, right| {
        left.location
            .path
            .cmp(&right.location.path)
            .then(left.location.line.cmp(&right.location.line))
            .then(left.location.column.cmp(&right.location.column))
    });
    program.types.sort_by(|left, right| {
        left.location
            .path
            .cmp(&right.location.path)
            .then(left.location.line.cmp(&right.location.line))
            .then(left.location.column.cmp(&right.location.column))
    });
    program.public_operations.sort_by(|left, right| {
        left.location
            .path
            .cmp(&right.location.path)
            .then(left.location.line.cmp(&right.location.line))
            .then(left.location.column.cmp(&right.location.column))
    });
    if stream { crate::stream::write(&serde_json::json!({"type": "syntax", "program": &program}))?; }
    if depth {
        program.depth = Some(crate::depth::collect_progress(&parsed, &program.failures, stream)?);
    }
    Ok(program)
}

fn source_failure_code(requested: &str, source_error: &str) -> String {
    if source_error.contains("escapes workspace") {
        return "SOURCE_PATH_ERROR".to_owned();
    }
    if requested.is_empty()
        || requested.contains('\\')
        || requested.starts_with('/')
        || requested
            .split('/')
            .any(|part| part.is_empty() || part == "." || part == "..")
    {
        return "SOURCE_PATH_ERROR".to_owned();
    }
    if !requested.ends_with(".rs") {
        return "UNSUPPORTED_SOURCE".to_owned();
    }
    "SOURCE_READ_ERROR".to_owned()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::{expression_kind, statement_kind};
    use std::time::{SystemTime, UNIX_EPOCH};

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
        let type_owners = TypeOwnerIndex::new(&program.types);
        collect_functions(
            &syntax.items,
            "sample.rs",
            &mut program.types,
            &type_owners,
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
        assert_eq!(service.method_locations.len(), 1);
        assert_eq!(service.method_fields["run"], vec!["state"]);
        assert_eq!(service.foreign_fields, vec!["other.value"]);
        assert!(service.foreign_types.contains(&"Peer".to_owned()));

        let mut test_functions = Vec::new();
        collect_functions(
            &syntax.items,
            "sample.rs",
            &mut program.types,
            &type_owners,
            &mut test_functions,
            true,
        );
        assert!(test_functions
            .iter()
            .any(|item| item.name == "inline_unit_test"));
    }

    #[test]
    fn owner_index_preserves_first_matching_declaration_semantics() {
        let syntax = syn::parse_file(
            r#"
            mod first { struct Shared { first: i32 } }
            mod second { struct Shared { second: i32 } }
            impl Shared { fn inspect(&self) -> i32 { self.first } }
            "#,
        )
        .expect("fixture parses");
        let mut types = Vec::new();
        declare_types(&syntax.items, "duplicate.rs", &mut types, false);
        let type_owners = TypeOwnerIndex::new(&types);
        let mut functions = Vec::new();
        collect_functions(
            &syntax.items,
            "duplicate.rs",
            &mut types,
            &type_owners,
            &mut functions,
            false,
        );

        assert_eq!(types[0].method_locations.len(), 1);
        assert!(types[1].method_locations.is_empty());
        assert_eq!(types[0].method_fields["inspect"], vec!["first"]);
    }

    #[test]
    fn retains_valid_rust_facts_and_locates_syntax_failures() {
        let workspace = syntax_fixture_workspace("mixed");
        fs::write(workspace.join("valid.rs"), "fn valid() { return; }\n").unwrap();
        fs::write(workspace.join("broken.rs"), "fn broken( { }\n").unwrap();
        let program = parse_program(
            &workspace,
            &["valid.rs".to_owned(), "broken.rs".to_owned()],
            false,
            false,
        )
        .unwrap();
        assert_eq!(program.files, vec!["valid.rs"]);
        assert_eq!(program.functions.len(), 1);
        assert_eq!(program.failures.len(), 1);
        assert_eq!(program.failures[0].path, "broken.rs");
        assert_eq!(program.failures[0].code, "SYNTAX_ERROR");
        assert!(program.failures[0].diagnostic.starts_with("broken.rs:1:"));
        assert!(program.failures[0].diagnostic.len() > "broken.rs:1:".len());
        assert!(program.unavailable["valid.rs"].contains_key("god_class"));
        let _ = fs::remove_dir_all(workspace);
    }

    #[test]
    fn reports_all_broken_rust_sources_without_partial_files() {
        let workspace = syntax_fixture_workspace("all-broken");
        fs::write(workspace.join("first.rs"), "fn first( { }\n").unwrap();
        fs::write(workspace.join("second.rs"), "fn second( { }\n").unwrap();
        let program = parse_program(
            &workspace,
            &["first.rs".to_owned(), "second.rs".to_owned()],
            false,
            false,
        )
        .unwrap();
        assert!(program.files.is_empty());
        assert!(program.functions.is_empty());
        assert_eq!(program.failures.len(), 2);
        assert!(program
            .failures
            .iter()
            .all(|item| item.code == "SYNTAX_ERROR"));
        let _ = fs::remove_dir_all(workspace);
    }

    #[test]
    fn retains_valid_rust_facts_when_sources_are_rejected() {
        let workspace = syntax_fixture_workspace("rejected");
        fs::write(workspace.join("valid.rs"), "fn valid() { return; }\n").unwrap();
        fs::write(workspace.join("notes.txt"), "not Rust\n").unwrap();
        let program = parse_program(
            &workspace,
            &[
                "missing.rs".to_owned(),
                "notes.txt".to_owned(),
                "valid.rs".to_owned(),
            ],
            false,
            false,
        )
        .unwrap();
        assert_eq!(program.files, vec!["valid.rs"]);
        assert_eq!(program.functions.len(), 1);
        assert_eq!(program.failures.len(), 2);
        assert!(program.failures.iter().all(|failure| {
            failure
                .diagnostic
                .starts_with(&format!("{}:", failure.path))
        }));
        assert!(program
            .failures
            .iter()
            .any(|failure| failure.path == "missing.rs" && failure.code == "SOURCE_READ_ERROR"));
        assert!(program
            .failures
            .iter()
            .any(|failure| failure.path == "notes.txt" && failure.code == "UNSUPPORTED_SOURCE"));
        let _ = fs::remove_dir_all(workspace);
    }

    fn syntax_fixture_workspace(name: &str) -> PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let workspace = std::env::temp_dir().join(format!("slopwatch-rust-{name}-{nanos}"));
        fs::create_dir_all(&workspace).unwrap();
        workspace
    }
}
