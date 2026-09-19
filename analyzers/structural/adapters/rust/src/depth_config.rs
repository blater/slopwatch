use syn::{Item, Meta};

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum CfgState {
    Enabled,
    Disabled,
    Unknown,
}

pub(super) fn item_cfg(item: &Item) -> CfgState {
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
pub(super) fn attrs_cfg(attrs: &[syn::Attribute]) -> CfgState {
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
