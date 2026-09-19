package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;

/** Receiver, dispatch, and package selection checks for Java calls. */
final class JavaDepthCallSelection {
    private JavaDepthCallSelection() { }

    static boolean sameOwner(ExecutableElement method, TypeElement expected) {
        return method != null && method.getEnclosingElement() instanceof TypeElement owner
                && owner.getQualifiedName().contentEquals(expected.getQualifiedName());
    }

    static boolean samePackage(ExecutableElement method, TypeElement expected) {
        if (method == null || !(method.getEnclosingElement() instanceof TypeElement owner)) return false;
        return packageName(owner).contentEquals(packageName(expected));
    }

    static boolean supportedSelection(Trees trees, TreePath root, MethodInvocationTree call,
                                      ExecutableElement method) {
        ExpressionTree select = call.getMethodSelect();
        if (select instanceof IdentifierTree) return true;
        if (method == null || !(select instanceof MemberSelectTree member)) return false;
        if (member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this")) return true;
        return method.getEnclosingElement() instanceof TypeElement type
                && isTypeQualifier(trees, root, member.getExpression(), type);
    }

    static boolean instanceSelection(MethodInvocationTree call) {
        ExpressionTree select = call.getMethodSelect();
        return select instanceof IdentifierTree
                || select instanceof MemberSelectTree member
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

    private static String packageName(TypeElement type) {
        Element enclosing = type;
        while (enclosing != null && enclosing.getKind() != ElementKind.PACKAGE) enclosing = enclosing.getEnclosingElement();
        return enclosing == null ? "" : enclosing.toString();
    }
}
