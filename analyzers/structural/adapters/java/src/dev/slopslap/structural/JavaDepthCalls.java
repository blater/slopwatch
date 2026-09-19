package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;

import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.nio.file.Path;

/** Attribution and shape checks for the deliberately small Java call subset. */
final class JavaDepthCalls {
    private JavaDepthCalls() { }

    static ExecutableElement resolve(Trees trees, TreePath root, MethodInvocationTree call) {
        Element element = trees.getElement(TreePath.getPath(root, call));
        return element instanceof ExecutableElement method ? method : null;
    }

    /** Indexes only methods backed by the supplied source units. */
    static Map<ExecutableElement, TreePath> sourceMethodPaths(Trees trees,
                                                               List<? extends com.sun.source.tree.CompilationUnitTree> units,
                                                               Path workspace,
                                                               Set<String> included) {
        Map<ExecutableElement, TreePath> result = new IdentityHashMap<>();
        for (var unit : units) {
            String file = workspace.relativize(Path.of(unit.getSourceFile().toUri())).toString().replace('\\', '/');
            if (included != null && !included.isEmpty() && !included.contains(file)) continue;
            new TreePathScanner<Void, Void>() {
                @Override public Void visitMethod(com.sun.source.tree.MethodTree tree, Void ignored) {
                    Element element = trees.getElement(getCurrentPath());
                    if (element instanceof ExecutableElement method) result.put(method, getCurrentPath());
                    return super.visitMethod(tree, ignored);
                }
            }.scan(unit, null);
        }
        return result;
    }

    static boolean supportedScalar(ExecutableElement method) {
        if (method == null || method.getKind() != ElementKind.METHOD) {
            return false;
        }
        return scalar(method.getReturnType()) && method.getParameters().stream()
                .allMatch(parameter -> scalar(parameter.asType()));
    }

    static boolean sameOwner(ExecutableElement method, TypeElement expected) {
        return method != null && method.getEnclosingElement() instanceof TypeElement owner
                && owner.getQualifiedName().contentEquals(expected.getQualifiedName());
    }

    static boolean samePackage(ExecutableElement method, TypeElement expected) {
        if (method == null || !(method.getEnclosingElement() instanceof TypeElement owner)) return false;
        return packageName(owner).contentEquals(packageName(expected));
    }

    private static String packageName(TypeElement type) {
        Element enclosing = type;
        while (enclosing != null && enclosing.getKind() != ElementKind.PACKAGE) {
            enclosing = enclosing.getEnclosingElement();
        }
        return enclosing == null ? "" : enclosing.toString();
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

    static boolean supportedSelection(Trees trees, TreePath root, MethodInvocationTree call,
                                      ExecutableElement method) {
        ExpressionTree select = call.getMethodSelect();
        if (select instanceof IdentifierTree) return true;
        if (method == null) return false;
        if (!(select instanceof MemberSelectTree member)) return false;
        if (member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this")) return true;
        Element owner = method.getEnclosingElement();
        if (!(owner instanceof TypeElement type)) return false;
        return isTypeQualifier(trees, root, member.getExpression(), type);
    }

    static boolean instanceSelection(MethodInvocationTree call) {
        ExpressionTree select = call.getMethodSelect();
        if (select instanceof IdentifierTree) return true;
        return select instanceof MemberSelectTree member
                && member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this");
    }

    static boolean staticSelection(Trees trees, TreePath root, MethodInvocationTree call,
                                   ExecutableElement method) {
        ExpressionTree select = call.getMethodSelect();
        if (select instanceof IdentifierTree) return true;
        if (!(select instanceof MemberSelectTree member) || method == null
                || !(method.getEnclosingElement() instanceof TypeElement owner)) return false;
        return isTypeQualifier(trees, root, member.getExpression(), owner);
    }

    static boolean dispatchable(ExecutableElement method, TypeElement owner) {
        return method != null && sameOwner(method, owner)
                && (owner.getModifiers().contains(Modifier.FINAL)
                || method.getModifiers().contains(Modifier.PRIVATE)
                || method.getModifiers().contains(Modifier.FINAL));
    }

    static boolean isTypeQualifier(Trees trees, TreePath root, ExpressionTree expression,
                                   TypeElement expected) {
        Element element = trees.getElement(TreePath.getPath(root, expression));
        return element instanceof TypeElement type
                && type.getQualifiedName().contentEquals(expected.getQualifiedName());
    }

    static String targetID(ExecutableElement method) {
        TypeElement owner = (TypeElement) method.getEnclosingElement();
        return owner.getQualifiedName() + "#" + method;
    }

    static boolean scalar(TypeMirror type) {
        if (type == null) return false;
        return type.getKind() == TypeKind.BYTE || type.getKind() == TypeKind.SHORT
                || type.getKind() == TypeKind.CHAR || type.getKind() == TypeKind.INT
                || type.getKind() == TypeKind.LONG || type.getKind() == TypeKind.BOOLEAN;
    }
}
