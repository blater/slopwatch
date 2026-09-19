package io.riverdb.sql;

/** Traverses canonical predicate operands at a derived-block boundary. */
final class SqlDerivedPredicateReferences {
  private static final int PROGRAMS_PER_LEAF = 4;

  boolean valid(SqlCommand block, SqlCommand inner) {
    if (!valid(block, inner, block.wherePredicates())) return false;
    SqlJoinChain joins = block.joinChain();
    if (joins == null) return true;
    for (int stage = 0; stage < joins.stageCount(); stage++) {
      if (!valid(block, inner, joins.onPredicates(stage))) return false;
    }
    return true;
  }

  private static boolean valid(
      SqlCommand block,
      SqlCommand inner,
      SqlBooleanPredicateProgram predicates) {
    for (int leaf = 0; leaf < predicates.leafCount(); leaf++) {
      for (int program = 0; program < PROGRAMS_PER_LEAF; program++) {
        if (!validProgram(block, inner, predicates, leaf, program)) return false;
      }
    }
    return true;
  }

  boolean references(SqlCommand command, CharSequence output) {
    SqlBooleanPredicateProgram predicates = command.wherePredicates();
    for (int leaf = 0; leaf < predicates.leafCount(); leaf++) {
      for (int program = 0; program < PROGRAMS_PER_LEAF; program++) {
        if (programReferences(command, predicates, leaf, program, output)) return true;
      }
    }
    return false;
  }

  private static boolean validProgram(
      SqlCommand block,
      SqlCommand inner,
      SqlBooleanPredicateProgram predicates,
      int leaf,
      int program) {
    for (int node = 0; node < predicates.programNodeCount(leaf, program); node++) {
      if (predicates.programOperator(leaf, program, node)
          != SqlScalarExpression.COLUMN) continue;
      int symbol = (int) predicates.programOperand(leaf, program, node);
      SqlIdentifier table = block.projections().symbolTable(symbol);
      SqlIdentifier name = block.projections().symbolName(symbol);
      if (table == null || name == null
          || !SqlDerivedColumnResolver.validQualifier(table, block)
          || inner != null && SqlDerivedColumnResolver.outputIndex(inner, name) < 0) {
        return false;
      }
    }
    return true;
  }

  private static boolean programReferences(
      SqlCommand command,
      SqlBooleanPredicateProgram predicates,
      int leaf,
      int program,
      CharSequence output) {
    for (int node = 0; node < predicates.programNodeCount(leaf, program); node++) {
      if (predicates.programOperator(leaf, program, node)
          != SqlScalarExpression.COLUMN) continue;
      SqlIdentifier name = command.projections().symbolName(
          (int) predicates.programOperand(leaf, program, node));
      if (name != null && SqlDerivedColumnResolver.sameName(name, output)) return true;
    }
    return false;
  }
}
