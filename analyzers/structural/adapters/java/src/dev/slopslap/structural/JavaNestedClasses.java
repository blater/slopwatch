package dev.slopslap.structural;

import com.sun.source.tree.ClassTree;
import com.sun.source.util.TreeScanner;

import java.util.List;

final class JavaNestedClasses extends TreeScanner<Void, Void> {
    private final List<ClassTree> output;

    JavaNestedClasses(List<ClassTree> output) {
        this.output = output;
    }

    @Override
    public Void visitClass(ClassTree node, Void unused) {
        output.add(node);
        return null;
    }
}
