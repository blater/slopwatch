package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.type.TypeMirror;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.nio.file.Path;

/** Attribution and shape checks for the deliberately small Java call subset. */
final class JavaDepthCalls {
    private JavaDepthCalls() { }

    static ExecutableElement resolve(Trees trees, TreePath root, MethodInvocationTree call) {
        return JavaDepthCallResolution.resolve(trees, root, call);
    }

    /** Indexes only methods backed by the supplied source units. */
    static Map<ExecutableElement, TreePath> sourceMethodPaths(Trees trees,
                                                               List<? extends com.sun.source.tree.CompilationUnitTree> units,
                                                               Path workspace,
                                                               Set<String> included) {
        return JavaDepthCallResolution.sourceMethodPaths(trees, units, workspace, included);
    }

    static boolean supportedScalar(ExecutableElement method) {
        return JavaDepthCallTypes.supportedScalar(method);
    }

    static boolean sameOwner(ExecutableElement method, TypeElement expected) {
        return JavaDepthCallSelection.sameOwner(method, expected);
    }

    static boolean samePackage(ExecutableElement method, TypeElement expected) {
        return JavaDepthCallSelection.samePackage(method, expected);
    }

    static boolean exactArgumentTypes(Trees trees, TreePath root, MethodInvocationTree call,
                                      ExecutableElement method) {
        return JavaDepthCallTypes.exactArgumentTypes(trees, root, call, method);
    }

    static boolean supportedSelection(Trees trees, TreePath root, MethodInvocationTree call,
                                      ExecutableElement method) {
        return JavaDepthCallSelection.supportedSelection(trees, root, call, method);
    }

    static boolean instanceSelection(MethodInvocationTree call) {
        return JavaDepthCallSelection.instanceSelection(call);
    }

    static boolean staticSelection(Trees trees, TreePath root, MethodInvocationTree call,
                                   ExecutableElement method) {
        return JavaDepthCallSelection.staticSelection(trees, root, call, method);
    }

    static boolean dispatchable(ExecutableElement method, TypeElement owner) {
        return JavaDepthCallSelection.dispatchable(method, owner);
    }

    static boolean isTypeQualifier(Trees trees, TreePath root, ExpressionTree expression,
                                   TypeElement expected) {
        return JavaDepthCallSelection.isTypeQualifier(trees, root, expression, expected);
    }

    static String targetID(ExecutableElement method) {
        return JavaDepthCallResolution.targetID(method);
    }

    static boolean scalar(TypeMirror type) {
        return JavaDepthCallTypes.scalar(type);
    }
}
