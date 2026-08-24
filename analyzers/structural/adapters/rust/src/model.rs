use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

pub const SCHEMA_VERSION: u32 = 2;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    pub schema_version: u32,
    pub workspace: String,
    pub source_paths: Vec<String>,
    pub include_tests: bool,
}

#[derive(Serialize)]
pub struct Response {
    pub schema_version: u32,
    pub program: Option<Program>,
    pub error: Option<String>,
}

#[derive(Clone, Default, Serialize)]
pub struct Location {
    pub path: String,
    pub line: usize,
    pub column: usize,
    pub end_line: usize,
    pub end_column: usize,
}

#[derive(Clone, Default, Serialize)]
pub struct Expression {
    pub kind: u8,
    pub children: Vec<Expression>,
    pub calls: Vec<String>,
    pub nested: Vec<Function>,
}

#[derive(Clone, Default, Serialize)]
pub struct Case {
    pub default: bool,
    pub falls_through: bool,
    pub expressions: Vec<Expression>,
    pub body: Vec<Statement>,
}

#[derive(Clone, Default, Serialize)]
pub struct Statement {
    pub kind: u8,
    pub location: Location,
    pub condition: Option<Expression>,
    pub expressions: Vec<Expression>,
    pub body: Vec<Statement>,
    #[serde(rename = "else")]
    pub else_body: Vec<Statement>,
    pub cases: Vec<Case>,
    pub may_skip: bool,
    pub labeled: bool,
}

#[derive(Clone, Default, Serialize)]
pub struct Function {
    pub name: String,
    pub receiver: String,
    pub receiver_var: String,
    pub location: Location,
    pub body: Vec<Statement>,
}

#[derive(Clone, Default, Serialize)]
pub struct TypeShape {
    pub stable_id: String,
    pub kind: String,
    pub name: String,
    pub children: Vec<TypeShape>,
    pub exposed_members: Vec<String>,
    pub complexity: usize,
}

#[derive(Clone, Default, Serialize)]
pub struct PublicOperation {
    pub stable_id: String,
    pub name: String,
    pub owner_type: String,
    pub location: Location,
    pub parameters: Vec<TypeShape>,
    pub results: Vec<TypeShape>,
    pub emits_output: bool,
    pub observable_mutation: bool,
}

#[derive(Clone, Default, Serialize)]
pub struct FieldFact {
    pub name: String,
    pub public: bool,
    pub mutable: bool,
    pub r#type: Option<TypeShape>,
}

#[derive(Clone, Default, Serialize)]
pub struct RepresentationExposure {
    pub stable_id: String,
    pub kind: String,
    pub entity: String,
    pub location: Location,
    pub evidence: String,
    pub confidence: String,
}

#[derive(Clone, Default, Serialize)]
pub struct TypeFact {
    pub name: String,
    pub kind: String,
    pub location: Location,
    pub method_locations: Vec<Location>,
    pub interface_method_count: usize,
    pub foreign_types: Vec<String>,
    pub method_fields: BTreeMap<String, Vec<String>>,
    pub foreign_fields: Vec<String>,
    #[serde(skip)]
    pub fields: Vec<String>,
    #[serde(rename = "fields")]
    pub field_facts: Vec<FieldFact>,
}

#[derive(Default, Serialize)]
pub struct Program {
    pub functions: Vec<Function>,
    pub types: Vec<TypeFact>,
    pub public_operations: Vec<PublicOperation>,
    #[serde(rename = "representation_exposure")]
    pub representation: Vec<RepresentationExposure>,
    pub files: Vec<String>,
    pub unavailable: BTreeMap<String, BTreeMap<String, String>>,
}

pub mod expression_kind {
    pub const OTHER: u8 = 0;
    pub const AND: u8 = 1;
    pub const OR: u8 = 2;
    pub const NOT: u8 = 3;
}

pub mod statement_kind {
    pub const LINEAR: u8 = 0;
    pub const BLOCK: u8 = 1;
    pub const IF: u8 = 2;
    pub const LOOP: u8 = 3;
    pub const SWITCH: u8 = 4;
    pub const RETURN: u8 = 5;
    pub const PANIC: u8 = 6;
    pub const BREAK: u8 = 7;
    pub const CONTINUE: u8 = 8;
}
