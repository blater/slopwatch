package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.SourcePositions;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;

import javax.tools.Diagnostic;
import java.util.*;

final class JavaAnalyzer extends TreePathScanner<Void, Void> {
    private final Facts.Program program = new Facts.Program();
    private final Trees trees;
    private final SourcePositions positions;
    private CompilationUnitTree unit;
    private String path;

    JavaAnalyzer(Trees trees) {
        this.trees = trees;
        this.positions = trees.getSourcePositions();
    }

    void scanUnit(CompilationUnitTree current, String relative) {
        unit = current;
        path = relative;
        program.files.add(relative);
        scan(current, null);
    }

    void skipUnit(String relative) {
        program.files.add(relative);
    }

    Facts.Program program() {
        return program;
    }

    void order() {
        Comparator<Facts.Location> locations = Comparator.comparing(Facts.Location::path)
                .thenComparingInt(Facts.Location::line).thenComparingInt(Facts.Location::column);
        program.functions.sort(Comparator.comparing(item -> item.location, locations));
        program.types.sort(Comparator.comparing(item -> item.location, locations));
        program.publicOperations.sort(Comparator.comparing(item -> item.location, locations));
        program.files.sort(String::compareTo);
    }

    Facts.Location location(Tree tree) {
        long start = positions.getStartPosition(unit, tree);
        long end = positions.getEndPosition(unit, tree);
        if (start == Diagnostic.NOPOS) {
            start = 0;
        }
        if (end == Diagnostic.NOPOS || end < start) {
            end = start;
        }
        LineMap lines = unit.getLineMap();
        return new Facts.Location(
                path, (int) lines.getLineNumber(start), (int) lines.getColumnNumber(start),
                (int) lines.getLineNumber(end), (int) lines.getColumnNumber(end)
        );
    }

    @Override
    public Void visitClass(ClassTree node, Void unused) {
        List<ClassTree> nestedClasses = new ArrayList<>();
        new JavaClassFacts(this, program, path).analyze(node, nestedClasses);
        nestedClasses.sort(Comparator.comparingLong(item -> positions.getStartPosition(unit, item)));
        nestedClasses.forEach(item -> scan(item, null));
        return null;
    }

    long startPosition(Tree tree) {
        return positions.getStartPosition(unit, tree);
    }

    List<Facts.Statement> statements(List<? extends StatementTree> input) {
        return new JavaStatements(this).translate(input);
    }

    Facts.Expression expression(ExpressionTree value) {
        return JavaExpressions.translate(this, value);
    }

}
