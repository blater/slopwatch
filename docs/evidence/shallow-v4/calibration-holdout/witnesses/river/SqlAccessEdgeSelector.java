package io.riverdb.engine.sql;

import io.riverdb.base.type.ExactDecimal;
import io.riverdb.base.type.SqlTypeDescriptor;
import io.riverdb.base.type.SqlValueDomain;
import io.riverdb.engine.relational.TableDefinition;
import io.riverdb.sql.SqlBooleanPredicateProgram;
import io.riverdb.sql.SqlComparison;
import io.riverdb.sql.SqlScalarExpression;

/** Selects one normalized physical edge from mandatory Boolean AND leaves. */
final class SqlAccessEdgeSelector {
  static final int MAXIMUM_EDGES = SqlBooleanPredicateProgram.MAXIMUM_LEAVES;
  final int[] columns = new int[MAXIMUM_EDGES];
  final int[] leaves = new int[MAXIMUM_EDGES];
  final SqlComparison[] comparisons = new SqlComparison[MAXIMUM_EDGES];
  final long[] values = new long[MAXIMUM_EDGES];
  final int[] descriptors = new int[MAXIMUM_EDGES];
  final long[] upperValues = new long[MAXIMUM_EDGES];
  final int[] upperDescriptors = new int[MAXIMUM_EDGES];
  final ExactDecimal.LongValue decimal = new ExactDecimal.LongValue();
  final ExactDecimal.WideScratch wide = new ExactDecimal.WideScratch();
  int count;
  long convertedValue;
  long normalizedLower;
  long normalizedUpper;
  private SqlBoundJoinContext joinContext;

  void select(
      SqlBooleanPredicateProgram source,
      SqlBoundBooleanPredicateProgram programs,
      BoundSqlStatement bound) {
    joinContext = null;
    count = 0;
    collect(source, programs, programs.root());
    bound.accessPredicate = -1;
    bound.predicateColumn = -1;
    bound.pointTextColumn = -1;
    bound.accessComparison = null;
    selectTextPoint(bound);
    int best = -1;
    for (int edge = 0; edge < count; edge++) {
      if (comparisons[edge] != SqlComparison.EQUAL) continue;
      int score = score(bound.table, columns[edge], comparisons[edge]);
      if (score > best && publish(bound, edge)) best = score;
    }
    if (best >= 3) {
      bound.pointTextColumn = -1;
    } else if (bound.pointTextColumn >= 0) {
      bound.accessPredicate = -1;
      bound.predicateColumn = -1;
      bound.accessComparison = null;
    } else {
      selectRange(bound, best);
    }
    clear();
  }

  void selectJoin(
      SqlBooleanPredicateProgram source,
      SqlBoundBooleanPredicateProgram programs,
      SqlBoundJoinContext context) {
    joinContext = context;
    count = 0;
    collect(source, programs, programs.root());
    context.accessPredicate = -1;
    context.predicateColumn = -1;
    context.accessComparison = null;
    int best = -1;
    TableDefinition table = context.table(0);
    for (int edge = 0; edge < count; edge++) {
      if (comparisons[edge] != SqlComparison.EQUAL) continue;
      int score = score(table, columns[edge], comparisons[edge]);
      if (score > best && publish(context, edge)) best = score;
    }
    selectRange(context, best);
    clear();
  }

  private void selectTextPoint(BoundSqlStatement bound) {
    for (int edge = 0; edge < count; edge++) {
      int column = columns[edge];
      if (comparisons[edge] == SqlComparison.EQUAL
          && bound.table.isVarchar(column)
          && bound.table.hasUniqueIndexOn(column)) {
        bound.pointTextColumn = column;
        return;
      }
    }
  }

  private void collect(
      SqlBooleanPredicateProgram source,
      SqlBoundBooleanPredicateProgram programs,
      int node) {
    SqlAccessEdgeCollection.collect(this, source, programs, node);
  }

  void collectLeaf(
      SqlBooleanPredicateProgram source,
      SqlBoundBooleanPredicateProgram programs,
      int leaf) {
    SqlAccessEdgeCollection.collectLeaf(this, source, programs, leaf);
  }

  private boolean publish(BoundSqlStatement bound, int edge) {
    int column = columns[edge];
    int target = bound.table.typeDescriptor(column);
    SqlComparison comparison = comparisons[edge];
    if (comparison == SqlComparison.HALF_OPEN_RANGE) {
      return normalizeRange(
              values[edge], descriptors[edge], SqlComparison.GREATER_OR_EQUAL,
              upperValues[edge], upperDescriptors[edge], SqlComparison.LESS_OR_EQUAL,
              target)
          && publishRange(bound, edge, column);
    }
    if (comparison != SqlComparison.EQUAL) return false;
    if (!convert(values[edge], descriptors[edge], target, comparison)) return false;
    long value = convertedValue;
    bound.accessPredicate = leaves[edge];
    bound.predicateColumn = column;
    bound.accessComparison = comparison;
    bound.accessValue = value;
    bound.accessLowerInclusive = value;
    bound.accessUpperExclusive = value;
    return true;
  }

  private boolean publish(SqlBoundJoinContext context, int edge) {
    int column = columns[edge];
    int target = context.table(0).typeDescriptor(column);
    SqlComparison comparison = comparisons[edge];
    if (comparison == SqlComparison.HALF_OPEN_RANGE) {
      return normalizeRange(
              values[edge], descriptors[edge], SqlComparison.GREATER_OR_EQUAL,
              upperValues[edge], upperDescriptors[edge], SqlComparison.LESS_OR_EQUAL,
              target)
          && publishRange(context, edge, column);
    }
    if (comparison != SqlComparison.EQUAL
        || !convert(values[edge], descriptors[edge], target, comparison)) {
      return false;
    }
    long value = convertedValue;
    context.accessPredicate = leaves[edge];
    context.predicateColumn = column;
    context.accessComparison = comparison;
    context.accessValue = value;
    context.accessLowerInclusive = value;
    context.accessUpperExclusive = value;
    return true;
  }

  private void selectRange(BoundSqlStatement bound, int previousBest) {
    int best = previousBest;
    for (int edge = 0; edge < count; edge++) {
      int column = columns[edge];
      int score = score(bound.table, column, SqlComparison.HALF_OPEN_RANGE);
      if (score <= best || !rangeForColumn(column, bound.table.typeDescriptor(column))) {
        continue;
      }
      bound.accessPredicate = leaves[edge];
      bound.predicateColumn = column;
      bound.accessComparison = SqlComparison.HALF_OPEN_RANGE;
      bound.accessLowerInclusive = normalizedLower;
      bound.accessUpperExclusive = normalizedUpper;
      best = score;
    }
  }

  private void selectRange(SqlBoundJoinContext context, int previousBest) {
    int best = previousBest;
    TableDefinition table = context.table(0);
    for (int edge = 0; edge < count; edge++) {
      int column = columns[edge];
      int score = score(table, column, SqlComparison.HALF_OPEN_RANGE);
      if (score <= best || !rangeForColumn(column, table.typeDescriptor(column))) {
        continue;
      }
      context.accessPredicate = leaves[edge];
      context.predicateColumn = column;
      context.accessComparison = SqlComparison.HALF_OPEN_RANGE;
      context.accessLowerInclusive = normalizedLower;
      context.accessUpperExclusive = normalizedUpper;
      best = score;
    }
  }

  private boolean rangeForColumn(int column, int target) {
    return SqlAccessEdgeNumeric.range(this, column, target);
  }

  private boolean publishRange(
      BoundSqlStatement bound, int edge, int column) {
    bound.accessPredicate = leaves[edge];
    bound.predicateColumn = column;
    bound.accessComparison = SqlComparison.HALF_OPEN_RANGE;
    bound.accessLowerInclusive = normalizedLower;
    bound.accessUpperExclusive = normalizedUpper;
    return true;
  }

  private boolean publishRange(
      SqlBoundJoinContext context, int edge, int column) {
    context.accessPredicate = leaves[edge];
    context.predicateColumn = column;
    context.accessComparison = SqlComparison.HALF_OPEN_RANGE;
    context.accessLowerInclusive = normalizedLower;
    context.accessUpperExclusive = normalizedUpper;
    return true;
  }

  private boolean normalizeRange(
      long lowerValue,
      int lowerDescriptor,
      SqlComparison lowerComparison,
      long upperValue,
      int upperDescriptor,
      SqlComparison upperComparison,
      int target) {
    return SqlAccessEdgeNumeric.normalizeRange(this, lowerValue, lowerDescriptor, lowerComparison,
        upperValue, upperDescriptor, upperComparison, target);
  }

  private boolean convert(
      long value, int source, int target, SqlComparison comparison) {
    return SqlAccessEdgeNumeric.convert(this, value, source, target, comparison);
  }

  private static int score(
      TableDefinition table, int column, SqlComparison comparison) {
    if (comparison != SqlComparison.EQUAL
        && comparison != SqlComparison.HALF_OPEN_RANGE) return -1;
    boolean indexed = column == 0
        || table.hasIndexOn(column) && !table.isVarchar(column);
    if (!indexed) return -1;
    if (comparison == SqlComparison.HALF_OPEN_RANGE) return 1;
    return column == 0 || table.hasUniqueIndexOn(column) ? 3 : 2;
  }


  static boolean lower(SqlComparison comparison) {
    return comparison == SqlComparison.GREATER_THAN
        || comparison == SqlComparison.GREATER_OR_EQUAL;
  }

  static boolean upper(SqlComparison comparison) {
    return comparison == SqlComparison.LESS_THAN
        || comparison == SqlComparison.LESS_OR_EQUAL;
  }

  boolean literal(
      SqlBooleanPredicateProgram source, int leaf, int program) {
    return source.programNodeCount(leaf, program) == 1
        && source.programOperator(leaf, program, 0) == SqlScalarExpression.LITERAL;
  }

  private void clear() {
    for (int edge = 0; edge < count; edge++) {
      columns[edge] = 0;
      leaves[edge] = 0;
      comparisons[edge] = null;
      values[edge] = 0;
      descriptors[edge] = 0;
      upperValues[edge] = 0;
      upperDescriptors[edge] = 0;
    }
    count = 0;
    joinContext = null;
  }

  boolean rootScope(int scope) {
    return joinContext == null
        ? scope == SqlBoundBooleanPredicateProgram.SCOPE_LEFT
        : joinContext.localRole(scope) == 0;
  }
}
