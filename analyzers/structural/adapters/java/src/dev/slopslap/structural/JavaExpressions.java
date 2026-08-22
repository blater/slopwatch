package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreeScanner;

final class JavaExpressions {
    private JavaExpressions() {}

    static Facts.Expression translate(JavaAnalyzer analyzer, ExpressionTree value) {
        if (value instanceof ParenthesizedTree parenthesized) return translate(analyzer, parenthesized.getExpression());
        Facts.Expression result = new Facts.Expression();
        if (value instanceof BinaryTree binary && (value.getKind() == Tree.Kind.CONDITIONAL_AND || value.getKind() == Tree.Kind.CONDITIONAL_OR)) {
            result.kind = value.getKind() == Tree.Kind.CONDITIONAL_AND ? Facts.EXPR_AND : Facts.EXPR_OR;
            result.children.add(translate(analyzer, binary.getLeftOperand()));
            result.children.add(translate(analyzer, binary.getRightOperand()));
            return result;
        }
        if (value instanceof UnaryTree unary && value.getKind() == Tree.Kind.LOGICAL_COMPLEMENT) {
            result.kind = Facts.EXPR_NOT;
            result.children.add(translate(analyzer, unary.getExpression()));
            return result;
        }
        if (value instanceof ConditionalExpressionTree conditional) {
            result.kind = Facts.EXPR_CONDITIONAL;
            result.children.add(translate(analyzer, conditional.getCondition()));
            result.children.add(translate(analyzer, conditional.getTrueExpression()));
            result.children.add(translate(analyzer, conditional.getFalseExpression()));
            return result;
        }
        new Metadata(analyzer, result).scan(value, null);
        result.calls.sort(String::compareTo);
        return result;
    }

    private static final class Metadata extends TreeScanner<Void, Void> {
        private final JavaAnalyzer analyzer;
        private final Facts.Expression result;

        private Metadata(JavaAnalyzer analyzer, Facts.Expression result) {
            this.analyzer = analyzer;
            this.result = result;
        }

        @Override
        public Void visitMethodInvocation(MethodInvocationTree node, Void unused) {
            if (node.getMethodSelect() instanceof IdentifierTree selected) result.calls.add(selected.getName().toString());
            else if (node.getMethodSelect() instanceof MemberSelectTree selected && selected.getExpression().toString().equals("this")) result.calls.add(selected.getIdentifier().toString());
            return super.visitMethodInvocation(node, unused);
        }

        @Override
        public Void visitConditionalExpression(ConditionalExpressionTree node, Void unused) {
            result.children.add(translate(analyzer, node)); return null;
        }

        @Override
        public Void visitBinary(BinaryTree node, Void unused) {
            if (node.getKind() == Tree.Kind.CONDITIONAL_AND || node.getKind() == Tree.Kind.CONDITIONAL_OR) {
                result.children.add(translate(analyzer, node)); return null;
            }
            return super.visitBinary(node, unused);
        }

        @Override
        public Void visitLambdaExpression(LambdaExpressionTree node, Void unused) {
            Facts.Function nested = new Facts.Function();
            nested.name = "<lambda>";
            nested.location = analyzer.location(node);
            if (node.getBody() instanceof BlockTree block) nested.body.addAll(analyzer.statements(block.getStatements()));
            else if (node.getBody() instanceof ExpressionTree body) {
                Facts.Statement statement = new Facts.Statement();
                statement.kind = Facts.STMT_LINEAR;
                statement.location = analyzer.location(body);
                statement.expressions.add(translate(analyzer, body));
                nested.body.add(statement);
            }
            result.nested.add(nested);
            return null;
        }
    }
}
