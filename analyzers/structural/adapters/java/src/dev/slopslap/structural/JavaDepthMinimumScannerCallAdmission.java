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

final class JavaDepthMinimumScannerCallAdmission {
    private final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerTraversal traversal;
    private final JavaDepthMinimumScannerServices services;
    private final Trees trees;
    private final Set<ExecutableElement> active;
    private final Set<String> sourceDelegations;
    private final TypeElement owner;
    private final Set<String> limitations;

    JavaDepthMinimumScannerCallAdmission(JavaDepthMinimumScannerCore context,
            JavaDepthMinimumScannerTraversal traversal,
            JavaDepthMinimumScannerServices services) {
        this.context = context;
        this.traversal = traversal;
        this.services = services;
        this.trees = context.trees;
        this.active = context.active;
        this.sourceDelegations = context.sourceDelegations;
        this.owner = context.owner;
        this.limitations = context.limitations;
    }
    private Void scan(Tree tree, Void unused) { return traversal.scan(tree, unused); }
    private TreePath getCurrentPath() { return traversal.currentPath(); }

    protected String delegateReceiverRoot(MethodInvocationTree invocation, ExecutableElement method) {
        if (method.getModifiers().contains(Modifier.STATIC)
                || owner.equals(method.getEnclosingElement()) && exactReceiver(invocation)) return "";
        if (invocation.getMethodSelect() instanceof MemberSelectTree select) {
            TreePath path = TreePath.getPath(getCurrentPath(), select.getExpression());
            if (path != null && trees.getElement(path) instanceof VariableElement field) {
                return JavaDepthRoles.fieldID(field) + "/delegate/";
            }
        }
        return "";
    }

    protected boolean sourceDelegate(MethodInvocationTree invocation, ExecutableElement method) {
        if (method.getKind() != ElementKind.METHOD || method.isVarArgs()) return false;
        boolean localOwner = owner.equals(method.getEnclosingElement());
        // Expand implementation helpers, not every public service they use.
        // Public API graphs can span most of a monorepo and must use shared
        // semantic summaries before they are eligible for body expansion.
        boolean finalDeclaringType = method.getEnclosingElement() instanceof TypeElement declaring
                && declaring.getModifiers().contains(Modifier.FINAL);
        boolean exactDispatch = method.getModifiers().contains(Modifier.STATIC)
                || method.getModifiers().contains(Modifier.PRIVATE)
                || method.getModifiers().contains(Modifier.FINAL) || finalDeclaringType;
        if (!exactDispatch) return false;
        if (!localOwner && (!JavaDepthCalls.samePackage(method, owner)
                || sourceDelegations.size() >= 16 || active.size() >= 8)) return false;
        TreePath declaration = trees.getPath(method);
        if (declaration == null || !(declaration.getLeaf() instanceof MethodTree body)
                || body.getBody() == null) return false;
        if (method.getModifiers().contains(Modifier.STATIC)) return true;
        if (!(method.getEnclosingElement() instanceof TypeElement declaring)) return false;
        boolean exact = method.getModifiers().contains(Modifier.PRIVATE)
                || method.getModifiers().contains(Modifier.FINAL)
                || declaring.getModifiers().contains(Modifier.FINAL);
        if (!exact) return false;
        if (owner.equals(declaring) && exactReceiver(invocation)) return true;
        if (!(invocation.getMethodSelect() instanceof MemberSelectTree select)) return false;
        TreePath receiverPath = TreePath.getPath(getCurrentPath(), select.getExpression());
        Element receiver = receiverPath == null ? null : trees.getElement(receiverPath);
        if (!(receiver instanceof VariableElement field) || !owner.equals(field.getEnclosingElement())
                || !field.getModifiers().containsAll(Set.of(Modifier.PRIVATE, Modifier.FINAL))
                || !services.ownField(select.getExpression())) return false;
        TreePath fieldPath = trees.getPath(field);
        if (fieldPath == null || !(fieldPath.getLeaf() instanceof VariableTree variable)
                || !(variable.getInitializer() instanceof NewClassTree allocation)
                || allocation.getClassBody() != null) return false;
        Element constructor = trees.getElement(TreePath.getPath(fieldPath, allocation));
        return constructor instanceof ExecutableElement created && declaring.equals(created.getEnclosingElement());
    }

    protected boolean exactReceiver(MethodInvocationTree tree) {
        ExpressionTree select = tree.getMethodSelect();
        if (select instanceof IdentifierTree) return true;
        return select instanceof MemberSelectTree member && JavaDepthMinimumBehavior.isThis(member.getExpression());
    }
}
