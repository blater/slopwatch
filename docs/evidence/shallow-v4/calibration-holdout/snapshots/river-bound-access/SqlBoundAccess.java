package io.riverdb.engine.sql;

import io.riverdb.sql.SqlComparison;

/** Primitive reusable access edge selected for one physical table source. */
class SqlBoundAccess {
  int predicateColumn;
  int accessPredicate;
  int pointTextColumn;
  long accessValue;
  long accessLowerInclusive;
  long accessUpperExclusive;
  SqlComparison accessComparison;

  void resetRootAccess() {
    predicateColumn = -1;
    accessPredicate = -1;
    pointTextColumn = -1;
    accessValue = 0;
    accessLowerInclusive = 0;
    accessUpperExclusive = 0;
    accessComparison = null;
  }
}
