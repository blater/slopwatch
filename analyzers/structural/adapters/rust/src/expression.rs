use crate::control::expression_body;
use crate::location::location;
use crate::model::{expression_kind, Expression, Function};
use syn::spanned::Spanned;
use syn::visit::{self, Visit};
use syn::{BinOp, Expr, ExprCall, ExprClosure, ExprMethodCall, ExprPath, UnOp};

struct Metadata<'a> {
    path: &'a str,
    calls: Vec<String>,
    nested: Vec<Function>,
}

impl<'ast> Visit<'ast> for Metadata<'_> {
    fn visit_expr_call(&mut self, node: &'ast ExprCall) {
        if let Expr::Path(path) = node.func.as_ref() {
            if let Some(segment) = path.path.segments.last() {
                self.calls.push(segment.ident.to_string());
            }
        }
        visit::visit_expr_call(self, node);
    }

    fn visit_expr_method_call(&mut self, node: &'ast ExprMethodCall) {
        self.calls.push(node.method.to_string());
        visit::visit_expr_method_call(self, node);
    }

    fn visit_expr_closure(&mut self, node: &'ast ExprClosure) {
        self.nested.push(Function {
            name: "<closure>".to_owned(),
            location: location(self.path, node.span()),
            body: expression_body(node.body.as_ref(), self.path),
            ..Function::default()
        });
    }
}

fn path_expression(expression: &Expr) -> Option<&ExprPath> {
    match expression {
        Expr::Path(path) => Some(path),
        _ => None,
    }
}

fn metadata(expression: &Expr, path: &str) -> (Vec<String>, Vec<Function>) {
    let mut visitor = Metadata {
        path,
        calls: Vec::new(),
        nested: Vec::new(),
    };
    visitor.visit_expr(expression);
    visitor.calls.sort();
    (visitor.calls, visitor.nested)
}

pub fn expression(value: &Expr, path: &str) -> Expression {
    match value {
        Expr::Binary(binary) if matches!(binary.op, BinOp::And(_)) => Expression {
            kind: expression_kind::AND,
            children: vec![
                expression(&binary.left, path),
                expression(&binary.right, path),
            ],
            ..Expression::default()
        },
        Expr::Binary(binary) if matches!(binary.op, BinOp::Or(_)) => Expression {
            kind: expression_kind::OR,
            children: vec![
                expression(&binary.left, path),
                expression(&binary.right, path),
            ],
            ..Expression::default()
        },
        Expr::Unary(unary) if matches!(unary.op, UnOp::Not(_)) => Expression {
            kind: expression_kind::NOT,
            children: vec![expression(&unary.expr, path)],
            ..Expression::default()
        },
        Expr::Group(group) => expression(&group.expr, path),
        Expr::Paren(paren) => expression(&paren.expr, path),
        _ => {
            let (calls, nested) = metadata(value, path);
            Expression {
                kind: expression_kind::OTHER,
                calls,
                nested,
                ..Expression::default()
            }
        }
    }
}

pub fn simple_path(expression: &Expr) -> Option<String> {
    path_expression(expression).map(|value| {
        value
            .path
            .segments
            .iter()
            .map(|part| part.ident.to_string())
            .collect::<Vec<_>>()
            .join("::")
    })
}
