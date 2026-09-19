package io.riverdb.sql;

import io.riverdb.base.error.StatusCode;

/** Validates names and reachability across adjacent derived projection blocks. */
final class SqlDerivedReferenceValidator {
  private final SqlQuery query;
  private final SqlDerivedPredicateReferences predicates =
      new SqlDerivedPredicateReferences();

  SqlDerivedReferenceValidator(SqlQuery ownedQuery) {
    query = ownedQuery;
  }

  StatusCode validate(int blockIndex, boolean allowUnusedComputed) {
    SqlCommand block = query.block(blockIndex);
    SqlCommand inner = blockIndex + 1 < query.sourceBlockCount()
        ? query.block(blockIndex + 1) : null;
    if (!validProjectionReferences(block, inner)
        || !predicates.valid(block, inner)
        || !validOrder(block, inner)) {
      return StatusCode.INVALID_EXTERNAL_INPUT;
    }
    return !allowUnusedComputed
            && blockIndex > 0
            && hasUnreachableComputed(blockIndex)
        ? StatusCode.FEATURE_NOT_SUPPORTED : StatusCode.OK;
  }

  private boolean hasUnreachableComputed(int blockIndex) {
    SqlCommand block = query.block(blockIndex);
    SqlCommand outer = query.block(blockIndex - 1);
    for (int projection = 0; projection < block.columnCount(); projection++) {
      SqlScalarExpression expression = block.projectionExpression(projection);
      if (expression != null && expression.isAvailable()
          && !expression.isDirectColumnReference()
          && !expression.isNullLiteral()
          && !references(outer, block.columnOutputName(projection))) {
        return true;
      }
    }
    return false;
  }

  private boolean references(SqlCommand outer, CharSequence output) {
    for (int projection = 0; projection < outer.columnCount(); projection++) {
      SqlScalarExpression expression = outer.projectionExpression(projection);
      if (expression != null && expression.isAvailable()
          && references(outer, expression, output)) return true;
    }
    return predicates.references(outer, output);
  }

  private static boolean references(
      SqlCommand command,
      SqlScalarExpression expression,
      CharSequence output) {
    for (int node = 0; node < expression.nodeCount(); node++) {
      if (expression.operator(node) != SqlScalarExpression.COLUMN) continue;
      SqlIdentifier name = command.projections().symbolName(
          (int) expression.operand(node));
      if (name != null && SqlDerivedColumnResolver.sameName(name, output)) {
        return true;
      }
    }
    return false;
  }

  private static boolean validProjectionReferences(
      SqlCommand block, SqlCommand inner) {
    if (loweredJoinAggregate(block, inner)) {
      for (int group = 0; group < block.grouping().count(); group++) {
        int projection = block.grouping().operandProjection(group);
        if (projection < 0 || projection >= inner.columnCount()) return false;
      }
      for (int invocation = 0;
          invocation < block.aggregates().invocationCount(); invocation++) {
        int lane = block.aggregates().operandProjection(invocation);
        if (lane >= inner.columnCount()) return false;
      }
      return true;
    }
    for (int projection = 0; projection < block.columnCount(); projection++) {
      SqlScalarExpression expression = block.projectionExpression(projection);
      if ((expression == null || !expression.isAvailable())
          && countOutput(block, projection)) continue;
      if (!validExpressionReferences(block, inner, expression)) {
        return false;
      }
    }
    for (int invocation = 0;
        invocation < block.aggregates().invocationCount(); invocation++) {
      int lane = block.aggregates().operandProjection(invocation);
      if (lane < 0) continue;
      if (!validExpressionReferences(
          block, inner, block.aggregateOperandExpression(lane))) {
        return false;
      }
    }
    return true;
  }

  private static boolean loweredJoinAggregate(
      SqlCommand block, SqlCommand inner) {
    return inner != null
        && block.joinChain() != null
        && inner.joinChain() != null
        && inner.type() == SqlCommandType.JOIN_SCAN
        && block.aggregates().invocationCount() > 0;
  }

  private static boolean countOutput(SqlCommand block, int projection) {
    int output = projection - (block.columnCount() - block.aggregates().outputCount());
    if (output < 0 || output >= block.aggregates().outputCount()) return false;
    int invocation = block.aggregates().outputInvocation(output);
    return invocation >= 0
        && block.aggregates().kind(invocation) == SqlAggregateKind.COUNT;
  }

  private static boolean validExpressionReferences(
      SqlCommand block,
      SqlCommand inner,
      SqlScalarExpression expression) {
    if (expression == null || !expression.isAvailable()) return false;
    for (int node = 0; node < expression.nodeCount(); node++) {
      if (expression.operator(node) != SqlScalarExpression.COLUMN) continue;
      int symbol = (int) expression.operand(node);
      SqlIdentifier table = block.projections().symbolTable(symbol);
      SqlIdentifier name = block.projections().symbolName(symbol);
      if (table == null || name == null
          || !SqlDerivedColumnResolver.validQualifier(table, block)
          || inner != null && !outputContains(inner, name)) {
        return false;
      }
    }
    return true;
  }

  private static boolean validOrder(SqlCommand block, SqlCommand inner) {
    if (inner == null || !(block.orderBy().count() > 0)) return true;
    for (int expression = 0;
        expression < block.orderBy().count(); expression++) {
      CharSequence name = block.orderBy().name(expression);
      if (!outputContains(inner, name)
          && SqlDerivedColumnResolver.outputIndex(block, name) < 0) return false;
    }
    return true;
  }

  private static boolean outputContains(SqlCommand command, CharSequence name) {
    return SqlDerivedColumnResolver.outputIndex(command, name) >= 0;
  }
}
