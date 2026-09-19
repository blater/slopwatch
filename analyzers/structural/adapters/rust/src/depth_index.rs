use super::config::{item_cfg, CfgState};
use super::{Alias, FunctionInfo, Index};
use std::collections::BTreeMap;
use syn::{Expr, Item, UseTree, Visibility};

pub(super) fn collect_scope<'a>(
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

pub(super) fn resolve_call(
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
pub(super) fn resolve_path(
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
pub(super) fn function_id(path: &str, module: &[String], name: &str) -> String {
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

pub(super) fn likely_crate_root(path: &str) -> bool {
    let basename = path.rsplit('/').next().unwrap_or(path);
    basename == "lib.rs"
        || basename == "main.rs"
        || path.split('/').any(|component| component == "bin")
}
