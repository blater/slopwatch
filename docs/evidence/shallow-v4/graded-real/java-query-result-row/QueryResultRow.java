package blater.nql.runner.sql.domain;

import blater.nql.domain.ScalarKind;

import lombok.AllArgsConstructor;

import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.Map;

import static blater.nql.domain.DateFormats.TIMESTAMP;

/*
 * Responsibility: Views one fetched query row over the live result
 * columns and exposes typed and string value access.
 *
 * The backing QueryColumns are reused and overwritten on each fetch, so
 * a QueryResultRow is only valid until the next fetch.
 */
@AllArgsConstructor
public class QueryResultRow {
  // columnName  value
  Map<String, QueryColumn> columnValues;

  public Map<String, QueryColumn> getColumnValues() {
    return columnValues;
  }

  /*
   * Typed value for the column, or null if absent or SQL NULL.
   */
  public Object getValue(final String columnName) {
    var column = columnValues.get(columnName);
    return column == null ? null : column.getColumnValue();
  }

  /*
   * Stringified column value used when writing to a hierarchy leaf.
   * Empty string when absent or null.
   */
  public String getStringValue(final String columnName) {
    final Object val = getValue(columnName);
    return switch (val) {
      case null -> "";
      case BigDecimal decimal -> decimal.toPlainString();
      case LocalDateTime time -> TIMESTAMP.format(time);
      default -> val.toString();
    };
  }

  public boolean isNull(final String columnName) {
    var column = columnValues.get(columnName);
    return column == null || column.columnValueIsNull();
  }

  public ScalarKind getScalarKind(final String columnName) {
    var column = columnValues.get(columnName);
    if (column == null) return ScalarKind.STRING;
    return switch (column.getSqlType()) {
      case INTEGER, LONG, SHORT, FLOAT, DOUBLE, NUMBER -> ScalarKind.NUMBER;
      case BOOLEAN -> ScalarKind.BOOLEAN;
      case STRING, DATE -> ScalarKind.STRING;
    };
  }
}
