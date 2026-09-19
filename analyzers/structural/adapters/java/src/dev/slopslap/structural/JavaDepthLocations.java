package dev.slopslap.structural;

import com.sun.source.tree.CompilationUnitTree;
import com.sun.source.tree.Tree;
import com.sun.source.util.SourcePositions;
import java.util.Map;

/** Stable source-span projection for Java depth facts. */
final class JavaDepthLocations {
    private JavaDepthLocations() { }
    static Map<String, Object> sourceLocation(CompilationUnitTree unit, SourcePositions positions,
                                               String file, Tree tree) {
        long start = positions.getStartPosition(unit, tree);
        long end = positions.getEndPosition(unit, tree);
        if (start < 0) start = 0;
        if (end < start) end = start;
        int line = line(unit, start), column = column(unit, start);
        return Map.of("path", file, "line", line, "column", column,
                "end_line", line(unit, end), "end_column", column(unit, end));
    }
    private static int line(CompilationUnitTree unit, long position) {
        return unit.getLineMap() == null ? 1 : (int) unit.getLineMap().getLineNumber(position);
    }
    private static int column(CompilationUnitTree unit, long position) {
        return unit.getLineMap() == null ? 1 : (int) unit.getLineMap().getColumnNumber(position);
    }
}
