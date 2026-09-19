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

final class JavaDepthMinimumScannerGuards {
    private final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerTraversal traversal;
    private final JavaDepthMinimumScannerServices services;
    private final Trees trees;
    private final JavaDepthComputationIdentity.Session computations;
    private final Set<String> limitations;
    private final JavaDepthMinimumBehavior.Summary result;
    private final TypeElement owner;
    private final Set<Element> connected;
    private final Map<Element, JavaDepthMinimumBehavior.ExprFacts> locals;
    private final Set<VariableElement> controllingFields;
    private final Map<VariableElement, Boolean> booleanState;

    JavaDepthMinimumScannerGuards(JavaDepthMinimumScannerCore context,
            JavaDepthMinimumScannerTraversal traversal,
            JavaDepthMinimumScannerServices services) {
        this.context = context;
        this.traversal = traversal;
        this.services = services;
        this.trees = context.trees;
        this.computations = context.computations;
        this.limitations = context.limitations;
        this.result = context.result;
        this.owner = context.owner;
        this.connected = context.connected;
        this.locals = context.locals;
        this.controllingFields = context.controllingFields;
        this.booleanState = context.booleanState;
    }
    private Void scan(Tree tree, Void unused) { return traversal.scan(tree, unused); }
    private TreePath getCurrentPath() { return traversal.currentPath(); }

    public Void visitReturn(ReturnTree tree, Void unused) {
        context.tick();
        if (tree.getExpression() != null) {
                JavaDepthMinimumBehavior.ExprFacts facts = services.expression(tree.getExpression(), getCurrentPath());
                if (facts.connected) {
                    if (facts.transform) {
                    result.returnedCategories.add("X");
                    services.transformation(tree.getExpression(), "return", false);
                }
                result.categories.addAll(facts.categories);
            }
        }
        Void value = traversal.scan(tree.getExpression(), unused);
        context.terminatePaths(false);
        return value;
    }

    public Void visitIf(IfTree tree, Void unused) {
        context.tick();
        Object constant = JavaDepthMinimumExpressions.constant(tree.getCondition(), getCurrentPath(), trees);
        if (constant instanceof Boolean selected) {
            traversal.scan(selected ? tree.getThenStatement() : tree.getElseStatement(), unused);
            return null;
        }
        JavaDepthMinimumBehavior.ExprFacts condition = services.expression(tree.getCondition(), getCurrentPath());
        Set<VariableElement> previous = new HashSet<>(controllingFields);
        controllingFields.addAll(condition.reads);
        Map<Element, JavaDepthMinimumBehavior.ExprFacts> before = new IdentityHashMap<>(locals);
        Set<Element> beforeConnected = new HashSet<>(connected);
        Map<VariableElement, Boolean> priorState = new IdentityHashMap<>(booleanState);
        traversal.scan(tree.getCondition(), unused);
        if ((condition.connected || guardConnected(tree.getCondition()))
                && (services.containsThrow(tree.getThenStatement())
                || containsStatusReturn(tree))) {
            // Both accepted and rejected values pass through this validation.
            context.credit("V", owner.getQualifiedName() + "/validation");
        }
        List<JavaDepthMinimumBehavior.PathAlternative> beforePaths = context.copyPaths(context.paths);
        assumeBoolean(tree.getCondition(), true);
        context.paths = context.copyPaths(beforePaths);
        traversal.scan(tree.getThenStatement(), unused);
        List<JavaDepthMinimumBehavior.PathAlternative> thenPaths = context.paths;
        Map<Element, JavaDepthMinimumBehavior.ExprFacts> thenLocals = new IdentityHashMap<>(locals);
        locals.clear(); locals.putAll(before);
        connected.clear(); connected.addAll(beforeConnected);
        booleanState.clear(); booleanState.putAll(priorState);
        context.paths = context.copyPaths(beforePaths);
        assumeBoolean(tree.getCondition(), false);
        traversal.scan(tree.getElseStatement(), unused);
        context.mergePaths(thenPaths, context.paths);
        // Retain only facts supported on both alternatives after a branch.
        locals.entrySet().removeIf(entry -> !context.sameExpressionFacts(entry.getValue(), thenLocals.get(entry.getKey())));
        connected.removeIf(element -> !element.getKind().isField() && !locals.containsKey(element)
                && !beforeConnected.contains(element));
        controllingFields.clear(); controllingFields.addAll(previous);
        booleanState.clear(); booleanState.putAll(priorState);
        return null;
    }

    protected boolean guardConnected(ExpressionTree condition) {
        TreePath path = TreePath.getPath(getCurrentPath(), condition);
        if (path == null) return false;
        final boolean[] found = {false};
        new TreePathScanner<Void, Void>() {
            public Void visitIdentifier(IdentifierTree tree, Void unused) {
                context.tick();
                Element element = trees.getElement(getCurrentPath());
                if (element != null && connected.contains(element)) found[0] = true;
                return super.visitIdentifier(tree, unused);
            }

            public Void visitMemberSelect(MemberSelectTree tree, Void unused) {
                context.tick();
                Element element = trees.getElement(getCurrentPath());
                if (element instanceof VariableElement field && owner.equals(field.getEnclosingElement())
                        && field.getConstantValue() == null && ownField(tree)) found[0] = true;
                return super.visitMemberSelect(tree, unused);
            }
        }.scan(path, null);
        return found[0];
    }

    protected boolean containsStatusReturn(IfTree tree) {
        List<ExpressionTree> thenReturns = returnExpressions(tree.getThenStatement());
        List<ExpressionTree> elseReturns = returnExpressions(tree.getElseStatement());
        if (contrastingReturns(thenReturns, elseReturns)) return true;
        if (thenReturns.isEmpty() == elseReturns.isEmpty()) return false;
        List<ExpressionTree> continuation = followingReturns();
        return contrastingReturns(thenReturns.isEmpty() ? elseReturns : thenReturns, continuation);
    }

    protected boolean contrastingReturns(List<ExpressionTree> first, List<ExpressionTree> second) {
        if (first.isEmpty() || second.isEmpty()) return false;
        TreePath parent = getCurrentPath();
        for (ExpressionTree left : first) {
            String leftKey = returnKey(left, parent);
            if (leftKey == null) continue;
            for (ExpressionTree right : second) {
                String rightKey = returnKey(right, parent);
                if (rightKey != null && !leftKey.equals(rightKey)) return true;
            }
        }
        return false;
    }

    protected List<ExpressionTree> returnExpressions(Tree tree) {
        if (tree == null) return List.of();
        List<ExpressionTree> result = new ArrayList<>();
        new TreeScanner<Void, Void>() {
            public Void visitReturn(ReturnTree node, Void unused) {
                if (node.getExpression() != null) result.add(node.getExpression());
                return null;
            }
            public Void visitClass(ClassTree node, Void unused) { return null; }
            public Void visitLambdaExpression(LambdaExpressionTree node, Void unused) { return null; }
        }.scan(tree, null);
        return result;
    }

    protected List<ExpressionTree> followingReturns() {
        TreePath current = getCurrentPath();
        TreePath parent = current == null ? null : current.getParentPath();
        if (parent == null || !(parent.getLeaf() instanceof BlockTree block)) return List.of();
        List<? extends StatementTree> statements = block.getStatements();
        int index = statements.indexOf(current.getLeaf());
        if (index < 0) return List.of();
        List<ExpressionTree> result = new ArrayList<>();
        for (int next = index + 1; next < statements.size(); next++) {
            result.addAll(returnExpressions(statements.get(next)));
        }
        return result;
    }

    protected String returnKey(ExpressionTree expression, TreePath parent) {
        Object constant = JavaDepthMinimumExpressions.constant(expression, parent, trees);
        if (constant != null) return "constant:" + constant.getClass().getName() + ":" + constant;
        TreePath path = TreePath.getPath(parent, expression);
        if (path == null) return expression.getKind() + ":" + expression;
        JavaDepthComputationIdentity.Result normalized = computations.assess(path, owner);
        if (normalized.identity() != null) return normalized.identity();
        Element element = trees.getElement(path);
        if (element instanceof VariableElement variable) {
            return "value:" + variable.getEnclosingElement() + "#" + variable.getSimpleName()
                    + ":" + variable.asType();
        }
        return expression.getKind() + ":" + expression;
    }

    protected void assumeBoolean(ExpressionTree condition, boolean value) {
        if (condition instanceof ParenthesizedTree wrapped) { assumeBoolean(wrapped.getExpression(), value); return; }
        if (condition instanceof UnaryTree unary && unary.getKind() == Tree.Kind.LOGICAL_COMPLEMENT) {
            assumeBoolean(unary.getExpression(), !value); return;
        }
        Element target = context.element(condition, getCurrentPath());
        if (target instanceof VariableElement field && owner.equals(field.getEnclosingElement())
                && field.asType().getKind() == TypeKind.BOOLEAN && ownField(condition)) booleanState.put(field, value);
    }

    protected boolean ownField(ExpressionTree tree) {
        return tree instanceof IdentifierTree || tree instanceof MemberSelectTree member && JavaDepthMinimumBehavior.isThis(member.getExpression());
    }
}
