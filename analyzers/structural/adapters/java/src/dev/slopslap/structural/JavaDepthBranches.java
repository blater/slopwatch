package dev.slopslap.structural;

import com.sun.source.tree.BinaryTree;
import com.sun.source.tree.BlockTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IfTree;
import com.sun.source.tree.ParenthesizedTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.UnaryTree;
import com.sun.source.tree.Tree;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/** Builds guarded branches while retaining short-circuit evaluation order. */
final class JavaDepthBranches {
    private final JavaDepthFunction host;
    private JavaDepthStatements statements;

    JavaDepthBranches(JavaDepthFunction host) { this.host = host; }

    void statements(JavaDepthStatements statements) { this.statements = statements; }

    void lower(IfTree tree) {
        JavaDepthFunction.Block yes = host.newBlock();
        JavaDepthFunction.Block no = host.newBlock();
        JavaDepthFunction.Block join = host.newBlock();
        lowerCondition(tree.getCondition(), yes, no);
        Map<javax.lang.model.element.Element, String> initial = new HashMap<>(host.values());
        JavaDepthFunction.BranchEnd left = lowerBranch(yes, tree.getThenStatement(), initial, join);
        JavaDepthFunction.BranchEnd right = lowerBranch(no, tree.getElseStatement(), initial, join);
        merge(join, initial, left, right);
    }

    private void merge(JavaDepthFunction.Block join, Map<javax.lang.model.element.Element, String> initial,
                       JavaDepthFunction.BranchEnd left, JavaDepthFunction.BranchEnd right) {
        host.currentBlock(join);
        host.values().clear();
        host.values().putAll(initial);
        if (!left.live() && !right.live()) return;
        if (!left.live()) { restore(right.values()); return; }
        if (!right.live()) { restore(left.values()); return; }
        for (Map.Entry<javax.lang.model.element.Element, String> entry : initial.entrySet()) mergeValue(entry, left, right);
    }

    private void restore(Map<javax.lang.model.element.Element, String> values) {
        host.values().clear();
        host.values().putAll(values);
    }

    private void mergeValue(Map.Entry<javax.lang.model.element.Element, String> entry,
                            JavaDepthFunction.BranchEnd left, JavaDepthFunction.BranchEnd right) {
        String leftValue = left.values().get(entry.getKey());
        String rightValue = right.values().get(entry.getKey());
        if (leftValue == null || rightValue == null) {
            host.values().put(entry.getKey(), host.unknownValue());
        } else if (leftValue.equals(rightValue)) {
            host.values().put(entry.getKey(), leftValue);
        } else {
            host.values().put(entry.getKey(), host.emitValue(Map.of("opcode", "phi",
                    "type", entry.getKey().asType().toString(),
                    "value_kind", JavaDepthTypes.kind(entry.getKey().asType()), "phi_inputs", List.of(
                            Map.of("predecessor", left.block().id, "value", leftValue),
                            Map.of("predecessor", right.block().id, "value", rightValue)))));
        }
    }

    private JavaDepthFunction.BranchEnd lowerBranch(JavaDepthFunction.Block block, StatementTree statement,
                                                     Map<javax.lang.model.element.Element, String> initial,
                                                     JavaDepthFunction.Block join) {
        host.currentBlock(block);
        restore(initial);
        if (statement != null) lowerStatement(statement);
        boolean live = !host.terminated();
        if (live) host.edge(host.currentBlock(), join, "normal", null);
        return new JavaDepthFunction.BranchEnd(host.currentBlock(), new HashMap<>(host.values()), live);
    }

    private void lowerStatement(StatementTree statement) {
        if (statement instanceof BlockTree body) statements.lower(body.getStatements());
        else if (statement instanceof IfTree nested) lower(nested);
        else if (statement instanceof ReturnTree returned && returned.getExpression() != null) {
            lowerReturn(returned.getExpression());
        } else statements.lower(List.of(statement));
    }

    private void lowerReturn(ExpressionTree expression) {
        statements.lowerReturn(expression);
    }

    void lowerCondition(ExpressionTree tree, JavaDepthFunction.Block yes, JavaDepthFunction.Block no) {
        if (tree instanceof ParenthesizedTree nested) {
            lowerCondition(nested.getExpression(), yes, no);
            return;
        }
        if (tree instanceof UnaryTree unary && unary.getKind() == Tree.Kind.LOGICAL_COMPLEMENT) {
            lowerCondition(unary.getExpression(), no, yes);
            return;
        }
        if (tree instanceof BinaryTree binary && shortCircuit(binary, Tree.Kind.CONDITIONAL_AND)) {
            JavaDepthFunction.Block right = host.newBlock();
            lowerCondition(binary.getLeftOperand(), right, no);
            host.currentBlock(right);
            lowerCondition(binary.getRightOperand(), yes, no);
            return;
        }
        if (tree instanceof BinaryTree binary && shortCircuit(binary, Tree.Kind.CONDITIONAL_OR)) {
            JavaDepthFunction.Block right = host.newBlock();
            lowerCondition(binary.getLeftOperand(), yes, right);
            host.currentBlock(right);
            lowerCondition(binary.getRightOperand(), yes, no);
            return;
        }
        String predicate = host.expressionValue(tree, host.typeOf(tree));
        host.edge(host.currentBlock(), yes, "true", predicate);
        host.edge(host.currentBlock(), no, "false", predicate);
    }

    private boolean shortCircuit(BinaryTree binary, Tree.Kind kind) { return binary.getKind() == kind; }

}
