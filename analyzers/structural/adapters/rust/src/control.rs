use crate::expression::expression;
use crate::location::location;
use crate::model::{statement_kind, Case, Statement};
use syn::spanned::Spanned;
use syn::{Block, Expr, Macro, Pat, Stmt};

fn statement(kind: u8, expression: &Expr, path: &str) -> Statement {
    Statement {
        kind,
        location: location(path, expression.span()),
        ..Statement::default()
    }
}

fn macro_name(value: &Macro) -> Option<String> {
    value
        .path
        .segments
        .last()
        .map(|segment| segment.ident.to_string())
}

fn else_body(expression: &Expr, path: &str) -> Vec<Statement> {
    match expression {
        Expr::Block(block) => statements(&block.block, path),
        _ => vec![expression_statement(expression, path)],
    }
}

fn expression_statement(value: &Expr, path: &str) -> Statement {
    match value {
        Expr::Block(block) => Statement {
            kind: statement_kind::BLOCK,
            location: location(path, value.span()),
            body: statements(&block.block, path),
            ..Statement::default()
        },
        Expr::Async(block) => Statement {
            kind: statement_kind::BLOCK,
            location: location(path, value.span()),
            body: statements(&block.block, path),
            ..Statement::default()
        },
        Expr::TryBlock(block) => Statement {
            kind: statement_kind::BLOCK,
            location: location(path, value.span()),
            body: statements(&block.block, path),
            ..Statement::default()
        },
        Expr::Unsafe(block) => Statement {
            kind: statement_kind::BLOCK,
            location: location(path, value.span()),
            body: statements(&block.block, path),
            ..Statement::default()
        },
        Expr::If(branch) => Statement {
            kind: statement_kind::IF,
            location: location(path, value.span()),
            condition: Some(expression(&branch.cond, path)),
            body: statements(&branch.then_branch, path),
            else_body: branch
                .else_branch
                .as_ref()
                .map(|(_, alternate)| else_body(alternate, path))
                .unwrap_or_default(),
            ..Statement::default()
        },
        Expr::While(looping) => Statement {
            kind: statement_kind::LOOP,
            location: location(path, value.span()),
            condition: Some(expression(&looping.cond, path)),
            body: statements(&looping.body, path),
            may_skip: true,
            ..Statement::default()
        },
        Expr::ForLoop(looping) => Statement {
            kind: statement_kind::LOOP,
            location: location(path, value.span()),
            condition: Some(expression(&looping.expr, path)),
            body: statements(&looping.body, path),
            may_skip: true,
            ..Statement::default()
        },
        Expr::Loop(looping) => Statement {
            kind: statement_kind::LOOP,
            location: location(path, value.span()),
            body: statements(&looping.body, path),
            ..Statement::default()
        },
        Expr::Match(branching) => Statement {
            kind: statement_kind::SWITCH,
            location: location(path, value.span()),
            condition: Some(expression(&branching.expr, path)),
            cases: branching
                .arms
                .iter()
                .map(|arm| Case {
                    default: matches!(arm.pat, Pat::Wild(_)),
                    expressions: arm
                        .guard
                        .as_ref()
                        .map(|(_, guard)| vec![expression(guard, path)])
                        .unwrap_or_default(),
                    body: expression_body(&arm.body, path),
                    ..Case::default()
                })
                .collect(),
            ..Statement::default()
        },
        Expr::Return(returning) => {
            let mut result = statement(statement_kind::RETURN, value, path);
            if let Some(value) = &returning.expr {
                result.expressions.push(expression(value, path));
            }
            result
        }
        Expr::Break(breaking) => {
            let mut result = statement(statement_kind::BREAK, value, path);
            result.labeled = breaking.label.is_some();
            if let Some(value) = &breaking.expr {
                result.expressions.push(expression(value, path));
            }
            result
        }
        Expr::Continue(continuing) => {
            let mut result = statement(statement_kind::CONTINUE, value, path);
            result.labeled = continuing.label.is_some();
            result
        }
        Expr::Macro(invocation)
            if matches!(
                macro_name(&invocation.mac).as_deref(),
                Some("panic" | "todo" | "unimplemented")
            ) =>
        {
            statement(statement_kind::PANIC, value, path)
        }
        _ => Statement {
            kind: statement_kind::LINEAR,
            location: location(path, value.span()),
            expressions: vec![expression(value, path)],
            ..Statement::default()
        },
    }
}

pub fn expression_body(value: &Expr, path: &str) -> Vec<Statement> {
    match value {
        Expr::Block(block) => statements(&block.block, path),
        _ => vec![expression_statement(value, path)],
    }
}

pub fn statements(block: &Block, path: &str) -> Vec<Statement> {
    block
        .stmts
        .iter()
        .filter_map(|value| match value {
            Stmt::Expr(expression) | Stmt::Semi(expression, _) => {
                Some(expression_statement(expression, path))
            }
            Stmt::Local(local) => local.init.as_ref().map(|(_, value)| Statement {
                kind: statement_kind::LINEAR,
                location: location(path, local.span()),
                expressions: vec![expression(value, path)],
                ..Statement::default()
            }),
            Stmt::Item(_) => None,
        })
        .collect()
}
