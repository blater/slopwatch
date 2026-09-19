package blater.nestql.runner.sql;

import blater.nestql.runner.sql.domain.QueryColumn;
import blater.nestql.runner.sql.domain.QueryResultRow;
import blater.nestql.util.Log;

import java.sql.ResultSet;
import java.sql.ResultSetMetaData;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.LinkedHashMap;

/*
 * Responsibility: Owns one open JDBC Statement and ResultSet and exposes
 * both indexed result values and the current row as a named QueryResultRow.
 */
public final class SqlRowCursor implements AutoCloseable {
  private final Statement statement;
  private final ResultSet resultSet;
  private final ResultSetMetaData metaData;
  private final QueryResultRow row;
  private boolean positionedOnRow = false;

  public SqlRowCursor(Statement statement, ResultSet resultSet) throws SQLException {
    this.statement = statement;
    this.resultSet = resultSet;
    this.metaData = resultSet.getMetaData();
    this.row = new QueryResultRow(new LinkedHashMap<>());

    for (int index = 1; index <= columnCount(); index++) {
      String name = columnLabel(index);
      row.getColumnValues().put(name, new QueryColumn(name, columnType(index), index));
    }
  }

  public boolean next() {
    try {
      positionedOnRow = resultSet.next();
      if (positionedOnRow) {
        for (QueryColumn col : row.getColumnValues().values()) {
          col.setValue(value(col.getColumnIndex()));
        }
      }
      return positionedOnRow;
    } catch (SQLException ex) {
      Log.fatal(SQLException.class, "Could not step to the next resultset row. " + ex.getMessage());
      return false;
    }
  }

  public QueryResultRow row() {
    return positionedOnRow ? row : null;
  }

  public int columnCount() {
    try {
      return metaData.getColumnCount();
    } catch (SQLException ex) {
      Log.fatal(SQLException.class, "Could not get column count. " + ex.getMessage());
      return -1;
    }
  }

  public String columnLabel(int index) {
    try {
      return metaData.getColumnLabel(index);
    } catch (SQLException ex) {
      Log.fatal(SQLException.class, "Could not get column label. " + ex.getMessage());
      return null;
    }
  }

  public int columnType(int index) {
    try {
      return metaData.getColumnType(index);
    } catch (SQLException ex) {
      Log.fatal(SQLException.class, "Could not get column type. " + ex.getMessage());
      return -1;
    }
  }

  public Object value(int index) {
    try {
      return resultSet.getObject(index);
    } catch (SQLException ex) {
      Log.fatal(SQLException.class, "Could not get value. " + ex.getMessage());
      return null;
    }
  }

  public String stringValue(int index) {
    try {
      return resultSet.getString(index);
    } catch (SQLException ex) {
      Log.fatal(SQLException.class, "Could not get string value. " + ex.getMessage());
      return null;
    }
  }

  @Override
  public void close() {
    try {
      resultSet.close();
    } catch (SQLException ex) {
      Log.warn("Exception closing the result set. ", ex);
    }
    try {
      statement.close();
    } catch (SQLException ex) {
      Log.warn("Exception closing the statement. ", ex);
    }
  }
}
