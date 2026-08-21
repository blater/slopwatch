use crate::design::{collect_functions, declare_types, normalize_types};
use crate::model::Program;
use std::fs;
use std::path::{Component, Path, PathBuf};

fn canonical_source(workspace: &Path, requested: &str) -> Result<(PathBuf, String), String> {
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
