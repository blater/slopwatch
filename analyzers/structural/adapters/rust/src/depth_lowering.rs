use super::index::resolve_call;
use super::types::*;
use super::{FunctionInfo, Index, Lowered};
use serde_json::{json, Value};
use std::collections::BTreeMap;
use syn::{Expr, Stmt, Type};

pub(super) fn lower_function(
    function: &FunctionInfo<'_>,
    index: &Index<'_>,
    by_id: &BTreeMap<String, usize>,
) -> Lowered {
    let item = function.function;
    if item.sig.asyncness.is_some()
        || item.sig.constness.is_some()
        || item.sig.unsafety.is_some()
        || item.sig.abi.is_some()
        || !item.sig.generics.params.is_empty()
        || item.sig.generics.where_clause.is_some()
    {
        return unsupported_flow(&function.id, item, "unsupported_signature_shape");
    }
    let mut formals = Vec::new();
    let mut values = BTreeMap::new();
    let mut value_types = BTreeMap::new();
    for (position, argument) in item.sig.inputs.iter().enumerate() {
        let Some(ty) = argument_type(argument) else {
            return unsupported_flow(&function.id, item, "unsupported_signature_shape");
        };
        let Some((_, name)) = concept_type(ty) else {
            return unsupported_flow(&function.id, item, "unsupported_surface_type");
        };
        let id = format!("arg{position}");
        formals.push(json!({"id": id, "path": format!("{}/arg{position}", function.id), "type": name, "value_kind": value_kind(ty)}));
        if let Some(argument_name) = argument_name(argument) {
            values.insert(argument_name.clone(), id);
            value_types.insert(argument_name, name);
        }
    }
    let Some(result) = result_type(item) else {
        return unsupported_flow(&function.id, item, "unsupported_signature_shape");
    };
    let Some((_, result_name)) = concept_type(result) else {
        return unsupported_flow(&function.id, item, "unsupported_surface_type");
    };
    let mut instructions = Vec::new();
    let mut returned = None;
    let mut calls = Vec::new();
    for (position, statement) in item.block.stmts.iter().enumerate() {
        let last = position + 1 == item.block.stmts.len();
        match statement {
            Stmt::Local(local) => {
                let Some(name) = local_name(local) else {
                    return unsupported_flow_with_calls(
                        &function.id,
                        item,
                        "unsupported_local_binding",
                        calls,
                    );
                };
                let Some((_, initializer)) = local.init.as_ref() else {
                    return unsupported_flow_with_calls(
                        &function.id,
                        item,
                        "unsupported_local_binding",
                        calls,
                    );
                };
                let Some((value, value_type)) = lower_expression(
                    initializer,
                    function,
                    index,
                    by_id,
                    &mut instructions,
                    &values,
                    &value_types,
                    None,
                    &mut calls,
                ) else {
                    return unsupported_flow_with_calls(
                        &function.id,
                        item,
                        "unsupported_expression",
                        calls,
                    );
                };
                values.insert(name.clone(), value);
                value_types.insert(name, value_type);
            }
            Stmt::Semi(Expr::Return(value), _) | Stmt::Expr(Expr::Return(value)) if last => {
                returned = value.expr.as_deref()
            }
            Stmt::Expr(expression) if last => returned = Some(expression),
            _ => {
                return unsupported_flow_with_calls(
                    &function.id,
                    item,
                    "unsupported_control_flow",
                    calls,
                )
            }
        }
    }
    let Some(expression) = returned else {
        return unsupported_flow_with_calls(&function.id, item, "unsupported_control_flow", calls);
    };
    let operand = match lower_expression(
        expression,
        function,
        index,
        by_id,
        &mut instructions,
        &values,
        &value_types,
        Some(result_name.as_str()),
        &mut calls,
    ) {
        Some((value, value_type)) if value_type == result_name => value,
        _ => {
            return unsupported_flow_with_calls(&function.id, item, "unsupported_expression", calls)
        }
    };
    instructions.push(json!({"id": "return", "opcode": "return", "operands": [operand]}));
    Lowered {
        flow: flow_json(&function.id, &formals, result, &instructions),
        supported: true,
        reason: None,
        calls,
    }
}

fn unsupported_flow(id: &str, function: &syn::ItemFn, reason: &'static str) -> Lowered {
    unsupported_flow_with_calls(id, function, reason, Vec::new())
}
fn unsupported_flow_with_calls(
    id: &str,
    function: &syn::ItemFn,
    reason: &'static str,
    calls: Vec<String>,
) -> Lowered {
    let fallback = Type::Verbatim(proc_macro2::TokenStream::new());
    let result = result_type(function).unwrap_or(&fallback);
    Lowered {
        flow: flow_json(
            id,
            &[],
            result,
            &[
                json!({"id": "n1", "opcode": "unknown", "type": "unknown", "value_kind": "unknown", "results": ["n1"]}),
                json!({"id": "return", "opcode": "return", "operands": ["n1"]}),
            ],
        ),
        supported: false,
        reason: Some(reason),
        calls,
    }
}
fn flow_json(id: &str, formals: &[Value], result: &Type, instructions: &[Value]) -> Value {
    let (_, result_name) = concept_type(result).unwrap_or(("unknown", "unknown".to_owned()));
    json!({"id": id, "formals": formals, "results": [{"id": "result0", "type": result_name, "value_kind": value_kind(result)}], "entry": "entry", "blocks": [{"id": "entry", "instructions": instructions}], "return_type": result_name, "return_concepts": [concept_type(result).map(|value| value.0).unwrap_or("unknown")]})
}

fn lower_expression(
    expression: &Expr,
    function: &FunctionInfo<'_>,
    index: &Index<'_>,
    by_id: &BTreeMap<String, usize>,
    instructions: &mut Vec<Value>,
    values: &BTreeMap<String, String>,
    value_types: &BTreeMap<String, String>,
    expected: Option<&str>,
    calls: &mut Vec<String>,
) -> Option<(String, String)> {
    match expression {
        Expr::Path(path) if path.path.segments.len() == 1 => {
            let name = path.path.segments.last()?.ident.to_string();
            let value = values.get(&name)?.clone();
            let value_type = value_types.get(&name)?.clone();
            if expected.is_some_and(|wanted| wanted != value_type) {
                return None;
            }
            Some((value, value_type))
        }
        Expr::Lit(literal) => {
            let value_type = expected?.to_owned();
            if primitive_type_name(&value_type).is_none() {
                return None;
            }
            let id = format!("n{}", instructions.len() + 1);
            let value = scalar_literal(&literal.lit, &value_type)?;
            instructions.push(json!({"id": id, "opcode": "constant", "type": value_type, "value_kind": value_kind_name(&value_type), "value": {"type": value_type, "value_kind": value_kind_name(&value_type), "constant": value}, "results": [id]}));
            Some((id, value_type))
        }
        Expr::Unary(value) if matches!(value.op, syn::UnOp::Not(_)) => {
            let (operand, operand_type) = lower_expression(
                &value.expr,
                function,
                index,
                by_id,
                instructions,
                values,
                value_types,
                Some("bool"),
                calls,
            )?;
            if operand_type != "bool" || expected.is_some_and(|wanted| wanted != "bool") {
                return None;
            }
            let id = format!("n{}", instructions.len() + 1);
            instructions.push(json!({"id": id, "opcode": "primitive", "type": "bool", "value_kind": "boolean", "operator": "!", "arithmetic_mode": "boolean", "operands": [operand], "results": [id]}));
            Some((id, "bool".to_owned()))
        }
        Expr::Call(call) => {
            let target = resolve_call(&function.module, &call.func, index, by_id)?;
            let target_info = index.functions.iter().find(|item| item.id == target)?;
            if call.args.len() != target_info.function.sig.inputs.len() {
                return None;
            }
            let mut operands = Vec::new();
            let mut bindings = Vec::new();
            for (position, argument) in call.args.iter().enumerate() {
                let ty = argument_type(&target_info.function.sig.inputs[position])?;
                let (_, expected_type) = concept_type(ty)?;
                let (operand, operand_type) = lower_expression(
                    argument,
                    function,
                    index,
                    by_id,
                    instructions,
                    values,
                    value_types,
                    Some(expected_type.as_str()),
                    calls,
                )?;
                if operand_type != expected_type {
                    return None;
                }
                operands.push(operand.clone());
                bindings.push(json!({"formal": format!("arg{position}"), "actual": operand}));
            }
            let result = result_type(target_info.function)?;
            let (_, result_name) = concept_type(result)?;
            if expected.is_some_and(|wanted| wanted != result_name) {
                return None;
            }
            let id = format!("n{}", instructions.len() + 1);
            instructions.push(json!({"id": id, "opcode": "call", "type": result_name, "value_kind": value_kind(result), "operands": operands, "results": [id], "call": {"targets": [target], "bindings": bindings}}));
            calls.push(target);
            Some((id, result_name))
        }
        Expr::MethodCall(call)
            if matches!(
                call.method.to_string().as_str(),
                "wrapping_add" | "wrapping_mul"
            ) && call.args.len() == 1 =>
        {
            let Expr::Path(receiver) = call.receiver.as_ref() else {
                return None;
            };
            if receiver.path.segments.len() != 1 {
                return None;
            }
            let receiver_name = receiver.path.segments.last()?.ident.to_string();
            let receiver_type = value_types.get(&receiver_name)?.clone();
            if !matches!(receiver_type.as_str(), "i32" | "i64")
                || expected.is_some_and(|wanted| wanted != receiver_type)
            {
                return None;
            }
            let (argument, argument_type) = lower_expression(
                &call.args[0],
                function,
                index,
                by_id,
                instructions,
                values,
                value_types,
                Some(&receiver_type),
                calls,
            )?;
            if argument_type != receiver_type {
                return None;
            }
            let id = format!("n{}", instructions.len() + 1);
            let operator = if call.method == "wrapping_add" {
                "+"
            } else {
                "*"
            };
            instructions.push(json!({"id": id, "opcode": "primitive", "type": receiver_type, "value_kind": "numeric", "operator": operator, "arithmetic_mode": "wrapping", "operands": [values.get(&receiver_name)?, argument], "results": [id]}));
            Some((id, receiver_type))
        }
        _ => None,
    }
}
