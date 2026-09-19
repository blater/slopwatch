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

final class JavaDepthMinimumScannerExceptionTypes {
    private final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerTraversal traversal;
    private final JavaDepthMinimumScannerServices services;
    private final Trees trees;
    private final Set<String> limitations;
    private final JavaDepthMinimumBehavior.Summary result;

    JavaDepthMinimumScannerExceptionTypes(JavaDepthMinimumScannerCore context,
            JavaDepthMinimumScannerTraversal traversal,
            JavaDepthMinimumScannerServices services) {
        this.context = context;
        this.traversal = traversal;
        this.services = services;
        this.trees = context.trees;
        this.limitations = context.limitations;
        this.result = context.result;
    }

    private Void scan(Tree tree, Void unused) { return traversal.scan(tree, unused); }
    private TreePath getCurrentPath() { return traversal.currentPath(); }

    protected TypeElement thrownType(ExpressionTree expression, TreePath parent) {
        TreePath path = TreePath.getPath(parent, expression);
        if (path == null) return null;
        TypeMirror mirror = trees.getTypeMirror(path);
        if (mirror instanceof DeclaredType declared && declared.asElement() instanceof TypeElement type) return type;
        return null;
    }

    protected boolean matchesCatch(JavaDepthMinimumBehavior.PathAlternative path, CatchTree catcher) {
        if (path.pendingExceptionType == null) {
            limitations.add(result.id + ": catch matching is unknown for unresolved thrown type");
            return false;
        }
        for (TypeElement caught : catchTypes(catcher)) {
            if (isSubtype(path.pendingExceptionType, caught, Collections.newSetFromMap(new IdentityHashMap<>()))) return true;
        }
        return false;
    }

    protected List<TypeElement> catchTypes(CatchTree catcher) {
        List<TypeElement> result = new ArrayList<>();
        Tree typeTree = catcher.getParameter().getType();
        if (typeTree instanceof UnionTypeTree union) {
            for (Tree alternative : union.getTypeAlternatives()) addCatchType(result, alternative);
        } else addCatchType(result, typeTree);
        return result;
    }

    protected void addCatchType(List<TypeElement> types, Tree typeTree) {
        TreePath path = TreePath.getPath(getCurrentPath(), typeTree);
        if (path == null) return;
        TypeMirror mirror = trees.getTypeMirror(path);
        if (mirror instanceof DeclaredType declared && declared.asElement() instanceof TypeElement type) types.add(type);
    }

    protected boolean isSubtype(TypeElement actual, TypeElement expected, Set<Element> seen) {
        if (!seen.add(actual)) return false;
        if (actual.equals(expected)) return true;
        TypeMirror superclass = actual.getSuperclass();
        if (superclass instanceof DeclaredType declared && declared.asElement() instanceof TypeElement parent
                && isSubtype(parent, expected, seen)) return true;
        for (TypeMirror implemented : actual.getInterfaces()) {
            if (implemented instanceof DeclaredType declared && declared.asElement() instanceof TypeElement parent
                    && isSubtype(parent, expected, seen)) return true;
        }
        return false;
    }
}
