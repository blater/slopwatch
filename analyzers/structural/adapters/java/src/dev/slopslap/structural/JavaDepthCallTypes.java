package dev.slopslap.structural;

import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;

/** Scalar-shape checks used by Java call and expression analysis. */
final class JavaDepthCallTypes {
    private JavaDepthCallTypes() { }

    static boolean supportedScalar(ExecutableElement method) {
        return method != null && method.getKind() == ElementKind.METHOD
                && scalar(method.getReturnType())
                && method.getParameters().stream().allMatch(parameter -> scalar(parameter.asType()));
    }

    static boolean exactArgumentTypes(Trees trees, TreePath root, MethodInvocationTree call,
                                      ExecutableElement method) {
        if (call.getArguments().size() != method.getParameters().size()) return false;
        for (int index = 0; index < call.getArguments().size(); index++) {
            TypeMirror actual = trees.getTypeMirror(TreePath.getPath(root, call.getArguments().get(index)));
            TypeMirror formal = method.getParameters().get(index).asType();
            if (!scalar(actual) || actual.getKind() != formal.getKind()) return false;
        }
        return true;
    }

    static boolean scalar(TypeMirror type) {
        if (type == null) return false;
        return switch (type.getKind()) {
            case BYTE, SHORT, CHAR, INT, LONG, BOOLEAN -> true;
            default -> false;
        };
    }
}
