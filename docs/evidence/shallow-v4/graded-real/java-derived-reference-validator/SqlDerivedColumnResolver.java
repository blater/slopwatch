package io.riverdb.sql;

/** Resolves derived output names onto physical root-table columns. */
final class SqlDerivedColumnResolver {
  private final SqlQuery query;

  SqlDerivedColumnResolver(SqlQuery ownedQuery) {
    query = ownedQuery;
  }

  int copy(int blockIndex, CharSequence name, SqlIdentifier destination) {
    CharSequence resolved = name;
    for (int sourceIndex = blockIndex + 1;
        sourceIndex < query.sourceBlockCount();
        sourceIndex++) {
      SqlCommand source = query.block(sourceIndex);
      int projection = outputIndex(source, resolved);
      if (projection < 0) return -1;
      SqlScalarExpression expression = source.projectionExpression(projection);
      if (expression == null || !expression.isAvailable()) return -1;
      if (expression.isNullLiteral()) return 1;
      if (!expression.isDirectColumnReference()) return 2;
      int symbol = (int) expression.operand(0);
      SqlIdentifier table = source.projections().symbolTable(symbol);
      SqlIdentifier column = source.projections().symbolName(symbol);
      if (table == null || column == null || !validQualifier(table, source)) {
        return -1;
      }
      resolved = column;
    }
    destination.copyFrom(resolved);
    return 0;
  }

  static boolean validQualifier(CharSequence qualifier, SqlCommand command) {
    if (qualifier.length() == 0) return true;
    SqlJoinChain joins = command.joinChain();
    if (joins == null) {
      return sameName(qualifier, command.tableName())
          || command.tableAlias().length() > 0
              && sameName(qualifier, command.tableAlias());
    }
    for (int role = 0; role < joins.roleCount(); role++) {
      if (sameName(qualifier, joins.tableName(role))
          || joins.alias(role).length() > 0
              && sameName(qualifier, joins.alias(role))) return true;
    }
    return false;
  }

  static int outputIndex(SqlCommand command, CharSequence name) {
    int found = -1;
    for (int index = 0; index < command.columnCount(); index++) {
      if (sameName(command.columnOutputName(index), name)) {
        if (found >= 0) return -2;
        found = index;
      }
    }
    return found;
  }

  static boolean sameName(CharSequence left, CharSequence right) {
    if (left.length() != right.length()) return false;
    for (int index = 0; index < left.length(); index++) {
      if (left.charAt(index) != right.charAt(index)) return false;
    }
    return true;
  }
}
