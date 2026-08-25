use crate::control::statements;
use crate::expression::simple_path;
use crate::location::location;
use crate::model::{Function, TypeFact};
use std::collections::{BTreeSet, HashMap, HashSet};
use syn::spanned::Spanned;
use syn::visit::{self, Visit};
use syn::{
    Attribute, ExprField, Fields, ImplItem, Item, Member, Signature, TraitItem, Type, TypePath,
};

fn excluded_test(attributes: &[Attribute], include_tests: bool) -> bool {
    !include_tests
        && attributes.iter().any(|attribute| {
            attribute.path.is_ident("test")
                || (attribute.path.is_ident("cfg")
                    && attribute.tokens.to_string().replace(' ', "") == "(test)")
        })
}

fn member_name(member: &Member) -> String {
    match member {
        Member::Named(name) => name.to_string(),
        Member::Unnamed(index) => index.index.to_string(),
    }
}

fn field_names(fields: &Fields) -> Vec<String> {
    fields
        .iter()
        .enumerate()
        .map(|(index, field)| {
            field
                .ident
                .as_ref()
                .map(ToString::to_string)
                .unwrap_or_else(|| index.to_string())
        })
        .collect()
}

struct TypeNames {
    own: String,
    names: BTreeSet<String>,
}

impl<'ast> Visit<'ast> for TypeNames {
    fn visit_type_path(&mut self, node: &'ast TypePath) {
        let name = node
            .path
            .segments
            .iter()
            .map(|part| part.ident.to_string())
            .collect::<Vec<_>>()
            .join("::");
        const BUILTINS: &[&str] = &[
            "Self", "bool", "char", "f32", "f64", "i8", "i16", "i32", "i64", "i128", "isize",
            "str", "u8", "u16", "u32", "u64", "u128", "usize",
        ];
        if name != self.own && !BUILTINS.contains(&name.as_str()) {
            self.names.insert(name);
        }
        visit::visit_type_path(self, node);
    }
}

fn signature_types(signature: &Signature, own: &str) -> Vec<String> {
    let mut visitor = TypeNames {
        own: own.to_owned(),
        names: BTreeSet::new(),
    };
    visitor.visit_signature(signature);
    visitor.names.into_iter().collect()
}

fn fields_types(fields: &Fields, own: &str) -> Vec<String> {
    let mut visitor = TypeNames {
        own: own.to_owned(),
        names: BTreeSet::new(),
    };
    for field in fields {
        visitor.visit_type(&field.ty);
    }
    visitor.names.into_iter().collect()
}

struct FieldUses<'a> {
    own_fields: &'a HashSet<String>,
    own: BTreeSet<String>,
    foreign: BTreeSet<String>,
}

impl<'ast> Visit<'ast> for FieldUses<'_> {
    fn visit_expr_field(&mut self, node: &'ast ExprField) {
        let field = member_name(&node.member);
        match simple_path(&node.base) {
            Some(base) if base == "self" && self.own_fields.contains(&field) => {
                self.own.insert(field);
            }
            Some(base) if base != "self" => {
                self.foreign.insert(format!("{base}.{field}"));
            }
            _ => {}
        }
        visit::visit_expr_field(self, node);
    }
}

fn method_fields(block: &syn::Block, fields: &HashSet<String>) -> (Vec<String>, Vec<String>) {
    let mut visitor = FieldUses {
        own_fields: fields,
        own: BTreeSet::new(),
        foreign: BTreeSet::new(),
    };
    visitor.visit_block(block);
    (
        visitor.own.into_iter().collect(),
        visitor.foreign.into_iter().collect(),
    )
}

pub struct TypeOwnerIndex {
    owners: HashMap<String, TypeOwner>,
}

struct TypeOwner {
    index: usize,
    fields: HashSet<String>,
}

impl TypeOwnerIndex {
    pub fn new(types: &[TypeFact]) -> Self {
        let mut owners = HashMap::with_capacity(types.len());
        for (index, item) in types.iter().enumerate() {
            // `find` previously selected the first declaration with a matching name.
            // Preserve that behavior when duplicate names occur in separate modules.
            owners
                .entry(item.name.clone())
                .or_insert_with(|| TypeOwner {
                    index,
                    fields: item.fields.iter().cloned().collect(),
                });
        }
        Self { owners }
    }
}

fn receiver_name(value: &Type) -> Option<String> {
    match value {
        Type::Path(path) => path.path.segments.last().map(|part| part.ident.to_string()),
        Type::Reference(reference) => receiver_name(&reference.elem),
        Type::Paren(paren) => receiver_name(&paren.elem),
        _ => None,
    }
}

fn method_function(
    name: &str,
    receiver: &str,
    span: proc_macro2::Span,
    block: &syn::Block,
    path: &str,
) -> Function {
    Function {
        name: name.to_owned(),
        receiver: receiver.to_owned(),
        receiver_var: "self".to_owned(),
        location: location(path, span),
        body: statements(block, path),
    }
}

pub fn declare_types(items: &[Item], path: &str, output: &mut Vec<TypeFact>, include_tests: bool) {
    for item in items {
        match item {
            Item::Struct(value) if !excluded_test(&value.attrs, include_tests) => {
                output.push(TypeFact {
                    name: value.ident.to_string(),
                    kind: "struct".to_owned(),
                    location: location(path, value.span()),
                    fields: field_names(&value.fields),
                    foreign_types: fields_types(&value.fields, &value.ident.to_string()),
                    ..TypeFact::default()
                })
            }
            Item::Enum(value) if !excluded_test(&value.attrs, include_tests) => {
                let fields = value
                    .variants
                    .iter()
                    .flat_map(|variant| field_names(&variant.fields))
                    .collect::<Vec<_>>();
                let mut foreign = BTreeSet::new();
                for variant in &value.variants {
                    foreign.extend(fields_types(&variant.fields, &value.ident.to_string()));
                }
                output.push(TypeFact {
                    name: value.ident.to_string(),
                    kind: "type".to_owned(),
                    location: location(path, value.span()),
                    fields,
                    foreign_types: foreign.into_iter().collect(),
                    ..TypeFact::default()
                });
            }
            Item::Trait(value) if !excluded_test(&value.attrs, include_tests) => {
                output.push(TypeFact {
                    name: value.ident.to_string(),
                    kind: "interface".to_owned(),
                    location: location(path, value.span()),
                    interface_method_count: value
                        .items
                        .iter()
                        .filter(|item| matches!(item, TraitItem::Method(_)))
                        .count(),
                    ..TypeFact::default()
                })
            }
            Item::Type(value) if !excluded_test(&value.attrs, include_tests) => {
                let mut names = TypeNames {
                    own: value.ident.to_string(),
                    names: BTreeSet::new(),
                };
                names.visit_type(&value.ty);
                output.push(TypeFact {
                    name: value.ident.to_string(),
                    kind: "type".to_owned(),
                    location: location(path, value.span()),
                    foreign_types: names.names.into_iter().collect(),
                    ..TypeFact::default()
                });
            }
            Item::Mod(module) if !excluded_test(&module.attrs, include_tests) => {
                if let Some((_, nested)) = &module.content {
                    declare_types(nested, path, output, include_tests);
                }
            }
            _ => {}
        }
    }
}

pub fn collect_functions(
    items: &[Item],
    path: &str,
    types: &mut Vec<TypeFact>,
    type_owners: &TypeOwnerIndex,
    functions: &mut Vec<Function>,
    include_tests: bool,
) {
    for item in items {
        collect_item(item, path, types, type_owners, functions, include_tests);
    }
}

fn collect_item(
    item: &Item,
    path: &str,
    types: &mut Vec<TypeFact>,
    type_owners: &TypeOwnerIndex,
    functions: &mut Vec<Function>,
    include_tests: bool,
) {
    match item {
        Item::Fn(value) if !excluded_test(&value.attrs, include_tests) => {
            functions.push(Function {
                name: value.sig.ident.to_string(),
                location: location(path, value.span()),
                body: statements(&value.block, path),
                ..Function::default()
            })
        }
        Item::Impl(value) if !excluded_test(&value.attrs, include_tests) => {
            collect_impl_functions(value, path, types, type_owners, functions, include_tests)
        }
        Item::Trait(value) if !excluded_test(&value.attrs, include_tests) => {
            collect_trait_functions(value, path, types, type_owners, functions)
        }
        Item::Mod(module) if !excluded_test(&module.attrs, include_tests) => {
            if let Some((_, nested)) = &module.content {
                collect_functions(nested, path, types, type_owners, functions, include_tests);
            }
        }
        _ => {}
    }
}

fn collect_impl_functions(
    value: &syn::ItemImpl,
    path: &str,
    types: &mut [TypeFact],
    type_owners: &TypeOwnerIndex,
    functions: &mut Vec<Function>,
    include_tests: bool,
) {
    let Some(receiver) = receiver_name(&value.self_ty) else {
        return;
    };
    for member in &value.items {
        let ImplItem::Method(method) = member else {
            continue;
        };
        if excluded_test(&method.attrs, include_tests) {
            continue;
        }
        let function = method_function(
            &method.sig.ident.to_string(),
            &receiver,
            method.span(),
            &method.block,
            path,
        );
        attach_impl_function(method, &receiver, &function, types, type_owners);
        functions.push(function);
    }
}

fn attach_impl_function(
    method: &syn::ImplItemMethod,
    receiver: &str,
    function: &Function,
    types: &mut [TypeFact],
    type_owners: &TypeOwnerIndex,
) {
    let Some(indexed_owner) = type_owners.owners.get(receiver) else {
        return;
    };
    let owner = &mut types[indexed_owner.index];
    let (fields, foreign) = method_fields(&method.block, &indexed_owner.fields);
    owner
        .method_fields
        .insert(method.sig.ident.to_string(), fields);
    owner.foreign_fields.extend(foreign);
    owner
        .foreign_types
        .extend(signature_types(&method.sig, receiver));
    owner.method_locations.push(function.location.clone());
}

fn collect_trait_functions(
    value: &syn::ItemTrait,
    path: &str,
    types: &mut [TypeFact],
    type_owners: &TypeOwnerIndex,
    functions: &mut Vec<Function>,
) {
    for member in &value.items {
        let TraitItem::Method(method) = member else {
            continue;
        };
        let Some(block) = &method.default else {
            continue;
        };
        let owner_name = value.ident.to_string();
        let function = method_function(
            &method.sig.ident.to_string(),
            &owner_name,
            method.span(),
            block,
            path,
        );
        if let Some(indexed_owner) = type_owners.owners.get(&owner_name) {
            types[indexed_owner.index]
                .method_locations
                .push(function.location.clone());
        }
        functions.push(function);
    }
}

pub fn normalize_types(types: &mut [TypeFact]) {
    for item in types {
        item.foreign_types.sort();
        item.foreign_types.dedup();
        item.foreign_fields.sort();
        item.foreign_fields.dedup();
    }
}
