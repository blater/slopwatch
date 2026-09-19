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

final class JavaDepthMinimumScannerFlow {
    private final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerTraversal traversal;
    private final JavaDepthMinimumScannerServices services;
    private final Trees trees;
    private final Set<String> limitations;
    private final JavaDepthMinimumBehavior.Summary result;
    private final TypeElement owner;
    private final JavaDepthMinimumMonitors monitors;
    private final Set<Element> connected;
    private final Map<Element, JavaDepthMinimumBehavior.ExprFacts> locals;
    private final Deque<Tree> breakTargets;

    private Void scan(Tree tree, Void unused) { return traversal.scan(tree, unused); }
    private TreePath getCurrentPath() { return traversal.currentPath(); }

    JavaDepthMinimumScannerFlow(JavaDepthMinimumScannerCore context,
            JavaDepthMinimumScannerTraversal traversal,
            JavaDepthMinimumScannerServices services) {
        this.context = context;
        this.traversal = traversal;
        this.services = services;
        this.trees = context.trees;
        this.limitations = context.limitations;
        this.result = context.result;
        this.owner = context.owner;
        this.monitors = context.monitors;
        this.connected = context.connected;
        this.locals = context.locals;
        this.breakTargets = context.breakTargets;
    }

    public Void visitBreak(BreakTree tree, Void unused) {
        context.tick();
        if (tree.getLabel() != null || breakTargets.isEmpty()) {
            context.unsupportedControl(tree);
            return null;
        }
        Tree target = breakTargets.peek();
        for (JavaDepthMinimumBehavior.PathAlternative path : context.paths) if (path.live) {
            path.live = false;
            path.jumpTarget = target;
        }
        return null;
    }

    public Void visitSwitch(SwitchTree tree, Void unused) {
        context.tick();
        traversal.scan(tree.getExpression(), unused);
        List<JavaDepthMinimumBehavior.PathAlternative> entry = context.copyPaths(context.paths);
        List<JavaDepthMinimumBehavior.PathAlternative> fallthrough = new ArrayList<>();
        List<JavaDepthMinimumBehavior.PathAlternative> exits = new ArrayList<>();
        boolean hasDefault = false;
        Object selector = JavaDepthMinimumExpressions.constant(tree.getExpression(), getCurrentPath(), trees);
        if (selector instanceof Character character) selector = (int) character;
        CaseTree selected = null, defaultCase = null;
        if (selector != null) {
            for (CaseTree branch : tree.getCases()) {
                if (branch.getExpressions().isEmpty()) defaultCase = branch;
                for (ExpressionTree label : branch.getExpressions()) {
                    Object value = JavaDepthMinimumExpressions.constant(label, getCurrentPath(), trees);
                    if (value instanceof Character character) value = (int) character;
                    if (Objects.equals(selector, value)) selected = branch;
                }
            }
            if (selected == null) selected = defaultCase;
        }
        breakTargets.push(tree);
        try {
            for (CaseTree branch : tree.getCases()) {
                if (branch.getExpression() == null) hasDefault = true;
                context.paths = new ArrayList<>();
                if (selector == null || branch == selected) context.paths.addAll(context.copyPaths(entry));
                context.paths.addAll(context.copyPaths(fallthrough));
                if (branch.getBody() != null) traversal.scan(branch.getBody(), unused);
                else traversal.scan(branch.getStatements(), unused);
                List<JavaDepthMinimumBehavior.PathAlternative> next = new ArrayList<>();
                for (JavaDepthMinimumBehavior.PathAlternative path : context.paths) {
                    if (path.jumpTarget == tree) {
                        path.jumpTarget = null;
                        path.live = true;
                        exits.add(path);
                    } else if (!path.live || path.jumpTarget != null) {
                        exits.add(path);
                    } else {
                        next.add(path);
                    }
                }
                if (branch.getBody() != null) exits.addAll(next);
                else fallthrough = next;
            }
        } finally {
            breakTargets.pop();
        }
        exits.addAll(fallthrough);
        if (!hasDefault && (selector == null || selected == null)) exits.addAll(context.copyPaths(entry));
        context.paths = context.deduplicatePaths(exits);
        return null;
    }

    public Void visitBinary(BinaryTree tree, Void unused) {
        if (tree.getKind() != Tree.Kind.CONDITIONAL_AND && tree.getKind() != Tree.Kind.CONDITIONAL_OR) {
            traversal.scan(tree.getLeftOperand(), unused); traversal.scan(tree.getRightOperand(), unused); return null;
        }
        context.tick();
        traversal.scan(tree.getLeftOperand(), unused);
        Object left = JavaDepthMinimumExpressions.constant(tree.getLeftOperand(), getCurrentPath(), trees);
        if (left instanceof Boolean value) {
            if (value == (tree.getKind() == Tree.Kind.CONDITIONAL_AND)) traversal.scan(tree.getRightOperand(), unused);
            return null;
        }
        List<JavaDepthMinimumBehavior.PathAlternative> skipped = context.copyPaths(context.paths);
        Map<Element, JavaDepthMinimumBehavior.ExprFacts> before = new IdentityHashMap<>(locals);
        Set<Element> beforeConnected = new HashSet<>(connected);
        traversal.scan(tree.getRightOperand(), unused);
        context.mergePaths(skipped, context.paths);
        locals.entrySet().removeIf(entry -> !context.sameExpressionFacts(entry.getValue(), before.get(entry.getKey())));
        connected.clear(); connected.addAll(beforeConnected);
        return null;
    }

    public Void visitConditionalExpression(ConditionalExpressionTree tree, Void unused) {
        context.tick();
        traversal.scan(tree.getCondition(), unused);
        Object value = JavaDepthMinimumExpressions.constant(tree.getCondition(), getCurrentPath(), trees);
        if (value instanceof Boolean selected) {
            traversal.scan(selected ? tree.getTrueExpression() : tree.getFalseExpression(), unused);
            return null;
        }
        List<JavaDepthMinimumBehavior.PathAlternative> beforePaths = context.copyPaths(context.paths);
        Map<Element, JavaDepthMinimumBehavior.ExprFacts> before = new IdentityHashMap<>(locals);
        Set<Element> beforeConnected = new HashSet<>(connected);
        traversal.scan(tree.getTrueExpression(), unused);
        List<JavaDepthMinimumBehavior.PathAlternative> truePaths = context.paths;
        Map<Element, JavaDepthMinimumBehavior.ExprFacts> trueLocals = new IdentityHashMap<>(locals);
        context.paths = context.copyPaths(beforePaths);
        locals.clear(); locals.putAll(before);
        connected.clear(); connected.addAll(beforeConnected);
        traversal.scan(tree.getFalseExpression(), unused);
        context.mergePaths(truePaths, context.paths);
        locals.entrySet().removeIf(entry -> !context.sameExpressionFacts(entry.getValue(), trueLocals.get(entry.getKey())));
        connected.clear(); connected.addAll(beforeConnected);
        return null;
    }

    public Void visitWhileLoop(WhileLoopTree tree, Void unused) {
        scanLoop(tree.getCondition(), tree.getStatement(), List.of(), tree, unused);
        return null;
    }

    public Void visitForLoop(ForLoopTree tree, Void unused) {
        traversal.scan(tree.getInitializer(), unused);
        scanLoop(tree.getCondition(), tree.getStatement(), tree.getUpdate(), tree, unused);
        return null;
    }

    public Void visitEnhancedForLoop(EnhancedForLoopTree tree, Void unused) {
        traversal.scan(tree.getExpression(), unused);
        traversal.scan(tree.getVariable(), unused);
        // The supplied collection may be empty.
        scanLoop(null, tree.getStatement(), List.of(), tree, unused);
        return null;
    }

    protected void scanLoop(ExpressionTree condition, StatementTree body,
                          List<? extends StatementTree> updates, Tree loop, Void unused) {
        context.tick();
        Object constant = condition == null ? null : JavaDepthMinimumExpressions.constant(condition, getCurrentPath(), trees);
        traversal.scan(condition, unused);
        if (Boolean.FALSE.equals(constant)) return;
        List<JavaDepthMinimumBehavior.PathAlternative> skipped = context.copyPaths(context.paths);
        Map<Element, JavaDepthMinimumBehavior.ExprFacts> before = new IdentityHashMap<>(locals);
        Set<Element> beforeConnected = new HashSet<>(connected);
        breakTargets.push(loop);
        try {
            traversal.scan(body, unused);
        } finally {
            breakTargets.pop();
        }
        traversal.scan(updates, unused);
        for (JavaDepthMinimumBehavior.PathAlternative path : context.paths) if (path.jumpTarget == loop) {
            path.jumpTarget = null;
            path.live = true;
        }
        if (!Boolean.TRUE.equals(constant)) context.mergePaths(skipped, context.paths);
        locals.entrySet().removeIf(entry -> !context.sameExpressionFacts(entry.getValue(), before.get(entry.getKey())));
        connected.clear(); connected.addAll(beforeConnected);
        limitations.add(result.id + ": loop uses bounded skipped/body alternatives; repeated iterations are not proved");
    }

    public Void visitSynchronized(SynchronizedTree tree, Void unused) {
        context.tick();
        int priorStateEffects = context.stateEffects;
        String monitor = monitors.monitor(tree.getExpression(), getCurrentPath());
        traversal.scan(tree.getExpression(), unused); traversal.scan(tree.getBlock(), unused);
        if (context.stateEffects > priorStateEffects) context.creditCoordination(monitor);
        return null;
    }
}
