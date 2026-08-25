package dev.slopslap.structural;

import com.sun.source.tree.*;

import java.util.List;

final class JavaControlStatements {
    private final JavaAnalyzer analyzer;
    private final JavaStatements statements;

    JavaControlStatements(JavaAnalyzer analyzer, JavaStatements statements) {
        this.analyzer = analyzer;
        this.statements = statements;
    }

    Facts.Statement statement(StatementTree value) {
        if (value instanceof BlockTree block) return block(value, block);
        if (value instanceof IfTree branch) return branch(value, branch);
        if (value instanceof WhileLoopTree loop)
            return loop(value, loop.getCondition(), loop.getStatement(), true);
        if (value instanceof DoWhileLoopTree loop)
            return loop(value, loop.getCondition(), loop.getStatement(), false);
        if (value instanceof ForLoopTree loop) return forLoop(value, loop);
        if (value instanceof EnhancedForLoopTree loop)
            return loop(value, loop.getExpression(), loop.getStatement(), true);
        if (value instanceof SwitchTree branching) return switchStatement(value, branching);
        if (value instanceof TryTree trying) return tryStatement(value, trying);
        if (value instanceof SynchronizedTree synchronizedTree)
            return synchronizedStatement(value, synchronizedTree);
        return null;
    }

    private Facts.Statement block(Tree source, BlockTree block) {
        Facts.Statement result = statements.base(Facts.STMT_BLOCK, source);
        result.body.addAll(statements.translate(block.getStatements()));
        return result;
    }

    private Facts.Statement branch(Tree source, IfTree branch) {
        Facts.Statement result = statements.base(Facts.STMT_IF, source);
        result.condition = analyzer.expression(branch.getCondition());
        addStatement(result.body, branch.getThenStatement());
        addStatement(result.elseBody, branch.getElseStatement());
        return result;
    }

    private Facts.Statement forLoop(Tree source, ForLoopTree loop) {
        Facts.Statement result = loop(source, loop.getCondition(), loop.getStatement(), true);
        loop.getInitializer().forEach(item -> addLoopExpression(result.expressions, item));
        loop.getUpdate().forEach(item -> result.expressions.add(analyzer.expression(item.getExpression())));
        return result;
    }

    private Facts.Statement switchStatement(Tree source, SwitchTree branching) {
        Facts.Statement result = statements.base(Facts.STMT_SWITCH, source);
        result.condition = analyzer.expression(branching.getExpression());
        branching.getCases().forEach(item -> result.cases.add(caseFact(item)));
        return result;
    }

    private Facts.Statement synchronizedStatement(Tree source, SynchronizedTree value) {
        Facts.Statement result = statements.base(Facts.STMT_BLOCK, source);
        result.expressions.add(analyzer.expression(value.getExpression()));
        result.body.addAll(statements.translate(value.getBlock().getStatements()));
        return result;
    }

    private Facts.Statement loop(Tree source, ExpressionTree condition, StatementTree body, boolean maySkip) {
        Facts.Statement result = statements.base(Facts.STMT_LOOP, source);
        if (condition != null) result.condition = analyzer.expression(condition);
        addStatement(result.body, body);
        result.maySkip = maySkip;
        return result;
    }

    private Facts.Statement tryStatement(Tree source, TryTree trying) {
        Facts.Statement result = statements.base(Facts.STMT_SWITCH, source);
        result.body.addAll(statements.translate(trying.getBlock().getStatements()));
        for (CatchTree caught : trying.getCatches()) {
            Facts.CaseFact branch = new Facts.CaseFact();
            branch.body.addAll(statements.translate(caught.getBlock().getStatements()));
            result.cases.add(branch);
        }
        if (trying.getFinallyBlock() != null)
            result.body.addAll(statements.translate(trying.getFinallyBlock().getStatements()));
        return result;
    }

    private void addStatement(List<Facts.Statement> output, StatementTree value) {
        if (value == null) return;
        if (value instanceof BlockTree block) output.addAll(statements.translate(block.getStatements()));
        else output.add(statements.statement(value));
    }

    private void addLoopExpression(List<Facts.Expression> output, StatementTree value) {
        if (value instanceof ExpressionStatementTree expression)
            output.add(analyzer.expression(expression.getExpression()));
        else if (value instanceof VariableTree variable && variable.getInitializer() != null)
            output.add(analyzer.expression(variable.getInitializer()));
    }

    private Facts.CaseFact caseFact(CaseTree value) {
        Facts.CaseFact result = new Facts.CaseFact();
        result.isDefault = value.getExpressions().isEmpty();
        value.getExpressions().forEach(item -> result.expressions.add(analyzer.expression(item)));
        if (value.getStatements() != null) result.body.addAll(statements.translate(value.getStatements()));
        else if (value.getBody() instanceof StatementTree body) result.body.add(statements.statement(body));
        else if (value.getBody() instanceof ExpressionTree body) result.body.add(statements.linear(body, body));
        return result;
    }
}
