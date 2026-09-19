package blater.nestql.runner.sql.query;

import blater.nestql.domain.Hierarchy;
import blater.nestql.parser.script.NestStatement;
import blater.nestql.runner.sql.SqlExecutor;
import blater.nestql.runner.sql.SqlRowCursor;
import blater.nestql.util.Template;

import java.util.Map;

public class RunQuery {

  public static Hierarchy runQuery(NestStatement stmt, Map<String, String> parameters, Hierarchy outputHierarchy, SqlExecutor sqlExecutor)
  {
    if (outputHierarchy == null)
      outputHierarchy = new Hierarchy();

    outputHierarchy.register(stmt);

    String querySql = Template.expand(stmt.getSql(), parameters);
    try (SqlRowCursor cursor = sqlExecutor.query(querySql)) {
      while (cursor.next()) {
        outputHierarchy.readRow(cursor.row());
      }
    }

    return outputHierarchy;
  }

}
