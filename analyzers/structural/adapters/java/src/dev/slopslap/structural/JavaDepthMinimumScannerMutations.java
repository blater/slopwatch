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

final class JavaDepthMinimumScannerMutations {
    private final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerTraversal traversal;
    private final JavaDepthMinimumScannerServices services;
    private final Trees trees;
    private final JavaDepthComputationIdentity.Session computations;
    private final JavaDepthMinimumBehavior.Summary result;
    private final TypeElement owner;
    private final Set<Element> connected;
    private final Map<Element, JavaDepthMinimumBehavior.ExprFacts> locals;
    private final Map<VariableElement, Boolean> booleanState;
    private final boolean constructor;

    private Void scan(Tree tree, Void unused) { return traversal.scan(tree, unused); }
    private TreePath getCurrentPath() { return traversal.currentPath(); }

    JavaDepthMinimumScannerMutations(JavaDepthMinimumScannerCore context,
            JavaDepthMinimumScannerTraversal traversal,
            JavaDepthMinimumScannerServices services) {
        this.context = context;
        this.traversal = traversal;
        this.services = services;
        this.trees = context.trees;
        this.computations = context.computations;
        this.result = context.result;
        this.owner = context.owner;
        this.connected = context.connected;
        this.locals = context.locals;
        this.booleanState = context.booleanState;
        this.constructor = context.constructor;
    }

    public Void visitVariable(VariableTree tree, Void unused) {
        context.tick();
        if (tree.getInitializer() != null) {
            JavaDepthMinimumBehavior.ExprFacts facts = services.expression(tree.getInitializer(), getCurrentPath());
            Element element = trees.getElement(getCurrentPath());
            if (element != null) {
                if (facts.connected) connected.add(element); else connected.remove(element);
                locals.put(element, facts);
            }
            traversal.scan(tree.getInitializer(), unused);
        }
        return null;
    }

    public Void visitAssignment(AssignmentTree tree, Void unused) {
        context.tick();
        JavaDepthMinimumBehavior.ExprFacts right = services.expression(tree.getExpression(), getCurrentPath());
        Element target = context.element(tree.getVariable(), getCurrentPath());
        if (target instanceof VariableElement field && owner.equals(field.getEnclosingElement()) && services.ownField(tree.getVariable())) {
            context.stateEffects++;
            if (right.connected && right.transform) {
                services.transformation(tree.getExpression(), JavaDepthRoles.fieldID(field), true);
            }
            if (right.connected) result.categories.addAll(right.categories);
            if (!constructor && scalarPrivate(field) && ((right.transform && right.reads.contains(field))
                    || booleanTransition(field, tree.getExpression()))) {
                context.credit("C", JavaDepthRoles.fieldID(field));
                result.addFieldRoot("C", field);
            }
        } else if (target != null && !target.getKind().isField()) {
            if (right.connected) connected.add(target); else connected.remove(target);
            locals.put(target, right);
        }
        traversal.scan(tree.getExpression(), unused);
        return null;
    }

    public Void visitCompoundAssignment(CompoundAssignmentTree tree, Void unused) {
        context.tick();
        if (JavaDepthMinimumExpressions.neutralUpdate(tree, getCurrentPath(), trees)) return null;
        JavaDepthMinimumBehavior.ExprFacts left = services.expression(tree.getVariable(), getCurrentPath());
        JavaDepthMinimumBehavior.ExprFacts right = services.expression(tree.getExpression(), getCurrentPath());
        Element target = context.element(tree.getVariable(), getCurrentPath());
        if (target instanceof VariableElement field && owner.equals(field.getEnclosingElement()) && services.ownField(tree.getVariable())) {
            context.stateEffects++;
            if (!constructor && scalarPrivate(field) && left.reads.contains(field)) {
                context.credit("C", JavaDepthRoles.fieldID(field));
                result.addFieldRoot("C", field);
            }
            if (left.connected || right.connected) {
                services.transformation(tree.getExpression(), JavaDepthRoles.fieldID(field), true);
            }
            if (right.connected) result.categories.addAll(right.categories);
        } else if (target != null && !target.getKind().isField()) {
            left.merge(right);
            left.transform = left.connected;
            locals.put(target, left);
            if (left.connected) connected.add(target); else connected.remove(target);
        }
        traversal.scan(tree.getExpression(), unused);
        return null;
    }

    public Void visitUnary(UnaryTree tree, Void unused) {
        context.tick();
        if (tree.getKind() == Tree.Kind.PREFIX_INCREMENT || tree.getKind() == Tree.Kind.PREFIX_DECREMENT
                || tree.getKind() == Tree.Kind.POSTFIX_INCREMENT || tree.getKind() == Tree.Kind.POSTFIX_DECREMENT) {
            Element target = context.element(tree.getExpression(), getCurrentPath());
            if (target instanceof VariableElement field && owner.equals(field.getEnclosingElement()) && services.ownField(tree.getExpression())) {
                context.stateEffects++;
                if (!constructor && scalarPrivate(field)) {
                    context.credit("C", JavaDepthRoles.fieldID(field));
                    result.addFieldRoot("C", field);
                }
                if (!constructor) {
                    services.transformation(tree.getExpression(), JavaDepthRoles.fieldID(field), true);
                }
            } else if (target != null && !target.getKind().isField()) {
                JavaDepthMinimumBehavior.ExprFacts value = services.expression(tree.getExpression(), getCurrentPath());
                value.transform = value.connected;
                locals.put(target, value);
            }
        }
        return traversal.scan(tree.getExpression(), unused);
    }

    protected boolean booleanTransition(VariableElement field, ExpressionTree value) {
        Object assigned = JavaDepthMinimumExpressions.constant(value, getCurrentPath(), trees);
        return assigned instanceof Boolean bool && booleanState.containsKey(field) && booleanState.get(field) != bool;
    }

    void transformation(ExpressionTree expression, String outcome, boolean effect) {
        TreePath path = TreePath.getPath(getCurrentPath(), expression);
        String computation = path == null ? null : computations.assess(path, owner).identity();
        if (computation == null) computation = "unresolved:" + result.id + ":" + expression;
        // A field's observable state outcome is one responsibility, regardless
        // of how many intermediate writes implement it.
        String root = effect ? owner.getQualifiedName() + "/state-transformation/" + outcome
                : owner.getQualifiedName() + "/computation/" + outcome + "/" + computation;
        result.transformationRoots.add(root);
        context.credit("X", root);
        if (effect) result.effectTransformationRoots.add(root);
    }

    protected boolean scalarPrivate(VariableElement field) {
        TypeKind kind = field.asType().getKind();
        return field.getModifiers().contains(Modifier.PRIVATE) && !field.getModifiers().contains(Modifier.STATIC)
                && (kind == TypeKind.BYTE || kind == TypeKind.SHORT || kind == TypeKind.INT
                || kind == TypeKind.LONG || kind == TypeKind.CHAR || kind == TypeKind.FLOAT
                || kind == TypeKind.DOUBLE || kind == TypeKind.BOOLEAN);
    }
}
