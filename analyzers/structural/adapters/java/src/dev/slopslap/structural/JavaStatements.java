package dev.slopslap.structural;

import com.sun.source.tree.*;
import java.util.ArrayList;
import java.util.List;

final class JavaStatements {
    private final JavaAnalyzer analyzer;

    JavaStatements(JavaAnalyzer analyzer) {
        this.analyzer = analyzer;
    }

    List<Facts.Statement> translate(List<? extends StatementTree> input) {
        List<Facts.Statement> result = new ArrayList<>(input.size());
        for (StatementTree value : input) {
            Facts.Statement translated = statement(value);
            if (translated != null) {
                result.add(translated);
            }
        }
        return result;
    }

    private Facts.Statement statement(StatementTree value) {
        if (value instanceof BlockTree block) {
            Facts.Statement result = base(Facts.STMT_BLOCK, value);
            result.body.addAll(translate(block.getStatements()));
            return result;
        }
        if (value instanceof IfTree branch) {
            Facts.Statement result = base(Facts.STMT_IF, value);
            result.condition = analyzer.expression(branch.getCondition());
            addStatement(result.body, branch.getThenStatement());
            addStatement(result.elseBody, branch.getElseStatement());
            return result;
        }
        if (value instanceof WhileLoopTree loop) {
            return loop(value, loop.getCondition(), loop.getStatement(), true);
        }
        if (value instanceof DoWhileLoopTree loop) {
            return loop(value, loop.getCondition(), loop.getStatement(), false);
        }
        if (value instanceof ForLoopTree loop) {
            Facts.Statement result = loop(value, loop.getCondition(), loop.getStatement(), true);
            loop.getInitializer().forEach(item -> addLoopExpression(result.expressions, item));
            loop.getUpdate().forEach(item ->
                    result.expressions.add(analyzer.expression(item.getExpression())));
            return result;
        }
        if (value instanceof EnhancedForLoopTree loop) {
            return loop(value, loop.getExpression(), loop.getStatement(), true);
        }
        if (value instanceof SwitchTree branching) {
            Facts.Statement result = base(Facts.STMT_SWITCH, value);
            result.condition = analyzer.expression(branching.getExpression());
            branching.getCases().forEach(item -> result.cases.add(caseFact(item)));
            return result;
        }
        if (value instanceof ReturnTree returning) {
            return exit(Facts.STMT_RETURN, value, returning.getExpression());
        }
        if (value instanceof ThrowTree throwing) {
            return exit(Facts.STMT_PANIC, value, throwing.getExpression());
        }
        if (value instanceof BreakTree breaking) {
            return jump(Facts.STMT_BREAK, value, breaking.getLabel() != null);
        }
        if (value instanceof ContinueTree continuing) {
            return jump(Facts.STMT_CONTINUE, value, continuing.getLabel() != null);
        }
        if (value instanceof TryTree trying) {
            return tryStatement(value, trying);
        }
        if (value instanceof SynchronizedTree synchronizedTree) {
            Facts.Statement result = base(Facts.STMT_BLOCK, value);
            result.expressions.add(analyzer.expression(synchronizedTree.getExpression()));
            result.body.addAll(translate(synchronizedTree.getBlock().getStatements()));
            return result;
        }
        if (value instanceof LabeledStatementTree labeled) {
            Facts.Statement result = statement(labeled.getStatement());
            result.labeled = true;
            return result;
        }
        if (value instanceof ExpressionStatementTree expression) {
            return linear(value, expression.getExpression());
        }
        if (value instanceof VariableTree variable && variable.getInitializer() != null) {
            return linear(value, variable.getInitializer());
        }
        return base(Facts.STMT_LINEAR, value);
    }

    private Facts.Statement base(int kind, Tree tree) {
        Facts.Statement result = new Facts.Statement();
        result.kind = kind;
        result.location = analyzer.location(tree);
        return result;
    }

    private Facts.Statement loop(Tree source, ExpressionTree condition, StatementTree body,
                                 boolean maySkip) {
        Facts.Statement result = base(Facts.STMT_LOOP, source);
        if (condition != null) {
            result.condition = analyzer.expression(condition);
        }
        addStatement(result.body, body);
        result.maySkip = maySkip;
        return result;
    }

    private Facts.Statement exit(int kind, Tree source, ExpressionTree expression) {
        Facts.Statement result = base(kind, source);
        if (expression != null) {
            result.expressions.add(analyzer.expression(expression));
        }
        return result;
    }

    private Facts.Statement jump(int kind, Tree source, boolean labeled) {
        Facts.Statement result = base(kind, source);
        result.labeled = labeled;
        return result;
    }

    private Facts.Statement linear(Tree source, ExpressionTree expression) {
        Facts.Statement result = base(Facts.STMT_LINEAR, source);
        result.expressions.add(analyzer.expression(expression));
        return result;
    }

    private Facts.Statement tryStatement(Tree source, TryTree trying) {
        Facts.Statement result = base(Facts.STMT_SWITCH, source);
        result.body.addAll(translate(trying.getBlock().getStatements()));
        for (CatchTree caught : trying.getCatches()) {
            Facts.CaseFact branch = new Facts.CaseFact();
            branch.body.addAll(translate(caught.getBlock().getStatements()));
            result.cases.add(branch);
        }
        if (trying.getFinallyBlock() != null) {
            result.body.addAll(translate(trying.getFinallyBlock().getStatements()));
        }
        return result;
    }

    private void addStatement(List<Facts.Statement> output, StatementTree value) {
        if (value == null) {
            return;
        }
        if (value instanceof BlockTree block) {
            output.addAll(translate(block.getStatements()));
        } else {
            output.add(statement(value));
        }
    }

    private void addLoopExpression(List<Facts.Expression> output, StatementTree value) {
        if (value instanceof ExpressionStatementTree expression) {
            output.add(analyzer.expression(expression.getExpression()));
        } else if (value instanceof VariableTree variable && variable.getInitializer() != null) {
            output.add(analyzer.expression(variable.getInitializer()));
        }
    }

    private Facts.CaseFact caseFact(CaseTree value) {
        Facts.CaseFact result = new Facts.CaseFact();
        result.isDefault = value.getExpressions().isEmpty();
        value.getExpressions().forEach(item ->
                result.expressions.add(analyzer.expression(item)));
        if (value.getStatements() != null) {
            result.body.addAll(translate(value.getStatements()));
        } else if (value.getBody() instanceof StatementTree body) {
            result.body.add(statement(body));
        } else if (value.getBody() instanceof ExpressionTree body) {
            result.body.add(linear(body, body));
        }
        return result;
    }
}
