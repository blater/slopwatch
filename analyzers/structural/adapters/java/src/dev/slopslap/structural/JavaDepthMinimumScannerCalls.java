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

final class JavaDepthMinimumScannerCalls {
    private final JavaDepthMinimumScannerCallAdmission admission;
    private final JavaDepthMinimumScannerCallEffects effects;

    JavaDepthMinimumScannerCalls(JavaDepthMinimumScannerCore context,
                                 JavaDepthMinimumScannerTraversal traversal,
                                 JavaDepthMinimumScannerServices services) {
        admission = new JavaDepthMinimumScannerCallAdmission(context, traversal, services);
        effects = new JavaDepthMinimumScannerCallEffects(context, traversal, services, admission);
    }

    public Void visitMethodInvocation(MethodInvocationTree tree, Void unused) {
        return effects.visitMethodInvocation(tree, unused);
    }
}
