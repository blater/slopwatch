package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.*;

final class JavaDepthMinimumScannerExceptions {
    private final JavaDepthMinimumScannerExceptionFlow flow;
    private final JavaDepthMinimumScannerExceptionTypes types;
    private final JavaDepthMinimumScannerServices services;

    JavaDepthMinimumScannerExceptions(JavaDepthMinimumScannerCore context,
                                      JavaDepthMinimumScannerTraversal traversal,
                                      JavaDepthMinimumScannerServices services) {
        this.services = services;
        types = new JavaDepthMinimumScannerExceptionTypes(context, traversal, services);
        flow = new JavaDepthMinimumScannerExceptionFlow(context, traversal, services, types);
    }

    public Void visitThrow(ThrowTree tree, Void unused) { return flow.visitThrow(tree, unused); }
    public Void visitTry(TryTree tree, Void unused) { return flow.visitTry(tree, unused); }
    TypeElement thrownType(ExpressionTree expression, TreePath parent) { return types.thrownType(expression, parent); }
}
