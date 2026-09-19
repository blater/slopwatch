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

final class JavaDepthMinimumBehaviorScanner extends TreePathScanner<Void, Void>
        implements JavaDepthMinimumScannerTraversal, JavaDepthMinimumScannerServices {
    final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerExpressions expressions;
    private final JavaDepthMinimumScannerExceptions exceptions;
    private final JavaDepthMinimumScannerGuards guards;
    private final JavaDepthMinimumScannerMutations mutations;
    private final JavaDepthMinimumScannerFlow flow;
    private final JavaDepthMinimumScannerCalls calls;

    JavaDepthMinimumBehaviorScanner(JavaDepthMinimumBehavior host,
                                     JavaDepthMinimumBehavior.Summary result,
                                     ExecutableElement method) {
        context = new JavaDepthMinimumScannerCore(host, result, method);
        expressions = new JavaDepthMinimumScannerExpressions(context, this, this);
        exceptions = new JavaDepthMinimumScannerExceptions(context, this, this);
        guards = new JavaDepthMinimumScannerGuards(context, this, this);
        mutations = new JavaDepthMinimumScannerMutations(context, this, this);
        flow = new JavaDepthMinimumScannerFlow(context, this, this);
        calls = new JavaDepthMinimumScannerCalls(context, this, this);
    }

    @Override public Void scan(Tree tree, Void unused) {
        if (tree == null) return null;
        if (tree instanceof StatementTree statement && !context.supportedStatement(statement.getKind())) {
            context.unsupportedControl(tree);
            return null;
        }
        return super.scan(tree, unused);
    }

    @Override public TreePath currentPath() { return super.getCurrentPath(); }
    @Override public Void scan(Iterable<? extends Tree> trees, Void unused) { return super.scan(trees, unused); }

    @Override public Void visitSwitchExpression(SwitchExpressionTree tree, Void unused) {
        context.unsupportedControl(tree); return null;
    }
    @Override public Void visitClass(ClassTree tree, Void unused) { return null; }
    @Override public Void visitLambdaExpression(LambdaExpressionTree tree, Void unused) { return null; }
    @Override public Void visitReturn(ReturnTree tree, Void unused) { return guards.visitReturn(tree, unused); }
    @Override public Void visitThrow(ThrowTree tree, Void unused) { return exceptions.visitThrow(tree, unused); }
    @Override public Void visitTry(TryTree tree, Void unused) { return exceptions.visitTry(tree, unused); }
    @Override public Void visitIf(IfTree tree, Void unused) { return guards.visitIf(tree, unused); }
    @Override public Void visitVariable(VariableTree tree, Void unused) { return mutations.visitVariable(tree, unused); }
    @Override public Void visitAssignment(AssignmentTree tree, Void unused) { return mutations.visitAssignment(tree, unused); }
    @Override public Void visitCompoundAssignment(CompoundAssignmentTree tree, Void unused) { return mutations.visitCompoundAssignment(tree, unused); }
    @Override public Void visitUnary(UnaryTree tree, Void unused) { return mutations.visitUnary(tree, unused); }
    @Override public Void visitBreak(BreakTree tree, Void unused) { return flow.visitBreak(tree, unused); }
    @Override public Void visitSwitch(SwitchTree tree, Void unused) { return flow.visitSwitch(tree, unused); }
    @Override public Void visitBinary(BinaryTree tree, Void unused) { return flow.visitBinary(tree, unused); }
    @Override public Void visitConditionalExpression(ConditionalExpressionTree tree, Void unused) { return flow.visitConditionalExpression(tree, unused); }
    @Override public Void visitWhileLoop(WhileLoopTree tree, Void unused) { return flow.visitWhileLoop(tree, unused); }
    @Override public Void visitForLoop(ForLoopTree tree, Void unused) { return flow.visitForLoop(tree, unused); }
    @Override public Void visitEnhancedForLoop(EnhancedForLoopTree tree, Void unused) { return flow.visitEnhancedForLoop(tree, unused); }
    @Override public Void visitSynchronized(SynchronizedTree tree, Void unused) { return flow.visitSynchronized(tree, unused); }
    @Override public Void visitMethodInvocation(MethodInvocationTree tree, Void unused) { return calls.visitMethodInvocation(tree, unused); }

    @Override public JavaDepthMinimumBehavior.ExprFacts expression(ExpressionTree tree, TreePath parent) {
        return expressions.expression(tree, parent);
    }
    @Override public boolean containsThrow(Tree tree) { return expressions.containsThrow(tree); }
    @Override public TypeElement thrownType(ExpressionTree expression, TreePath parent) {
        return exceptions.thrownType(expression, parent);
    }
    @Override public boolean recognizedError(ExpressionTree expression, TreePath parent) {
        return expressions.recognizedError(expression, parent);
    }
    @Override public void transformation(ExpressionTree expression, String outcome, boolean effect) {
        mutations.transformation(expression, outcome, effect);
    }
    @Override public boolean ownField(ExpressionTree tree) { return guards.ownField(tree); }
    void creditCoordination(String monitor) { context.creditCoordination(monitor); }
}

final class WorkLimit extends RuntimeException { }
