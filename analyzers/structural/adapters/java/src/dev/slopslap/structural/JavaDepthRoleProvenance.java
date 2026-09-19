package dev.slopslap.structural;

import com.sun.source.tree.CompilationUnitTree;
import com.sun.source.tree.Tree;
import com.sun.source.util.SourcePositions;
import com.sun.source.util.Trees;
import java.util.Map;

final class JavaDepthRoleProvenance {
    private final JavaDepthRoles owner;
    private final SourcePositions positions;

    JavaDepthRoleProvenance(JavaDepthRoles owner) {
        this.owner = owner;
        this.positions = owner.trees.getSourcePositions();
    }

    Map<String, Object> evidence(JavaDepthRoles.SourceType source, Tree tree,
                                 javax.lang.model.element.ExecutableElement method) {
        return Map.of("artifact", source.element().getEnclosingElement().toString(), "path", source.file(),
                "span", location(source.unit(), tree), "rule_id", JavaDepthRoles.RULE,
                "fact_ids", java.util.List.of(JavaDepthRoles.methodID(method)), "omitted_location_count", 0);
    }

    private Map<String, Object> location(CompilationUnitTree unit, Tree tree) {
        long start = Math.max(0, positions.getStartPosition(unit, tree));
        long end = Math.max(start, positions.getEndPosition(unit, tree));
        var lineMap = unit.getLineMap();
        int line = lineNumber(lineMap, start, 1);
        int column = columnNumber(lineMap, start, 1);
        int endLine = lineNumber(lineMap, end, line);
        int endColumn = columnNumber(lineMap, end, column);
        return Map.of("path", relativePath(unit), "line", line, "column", column,
                "end_line", endLine, "end_column", endColumn);
    }

    private int lineNumber(com.sun.source.tree.LineMap map, long offset, int fallback) {
        return map == null ? fallback : (int) map.getLineNumber(offset);
    }

    private int columnNumber(com.sun.source.tree.LineMap map, long offset, int fallback) {
        return map == null ? fallback : (int) map.getColumnNumber(offset);
    }

    private String relativePath(CompilationUnitTree unit) {
        for (JavaDepthRoles.SourceType source : owner.sources) if (source.unit() == unit) return source.file();
        return unit.getSourceFile().getName();
    }
}
