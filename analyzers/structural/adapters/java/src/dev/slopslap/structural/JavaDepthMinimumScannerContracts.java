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

interface JavaDepthMinimumScannerTraversal {
    Void scan(Tree tree, Void unused);
    Void scan(Iterable<? extends Tree> trees, Void unused);
    TreePath currentPath();
}

interface JavaDepthMinimumScannerServices {
    JavaDepthMinimumBehavior.ExprFacts expression(ExpressionTree tree, TreePath parent);
    boolean containsThrow(Tree tree);
    TypeElement thrownType(ExpressionTree expression, TreePath parent);
    boolean recognizedError(ExpressionTree expression, TreePath parent);
    void transformation(ExpressionTree expression, String outcome, boolean effect);
    boolean ownField(ExpressionTree tree);
}
