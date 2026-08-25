package dev.slopslap.structural;

import com.sun.source.tree.*;
import java.util.ArrayList;
import java.util.List;

final class JavaStatements {
    private final JavaAnalyzer analyzer;
	private final JavaControlStatements control;

    JavaStatements(JavaAnalyzer analyzer) {
        this.analyzer = analyzer;
		this.control = new JavaControlStatements(analyzer, this);
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

	Facts.Statement statement(StatementTree value) {
		Facts.Statement structured = control.statement(value);
        if (structured != null) return structured;
        Facts.Statement exit = exitStatement(value);
        if (exit != null) return exit;
        return simpleStatement(value);
    }

    private Facts.Statement exitStatement(StatementTree value) {
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
        return null;
    }

    private Facts.Statement simpleStatement(StatementTree value) {
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

	Facts.Statement base(int kind, Tree tree) {
        Facts.Statement result = new Facts.Statement();
        result.kind = kind;
        result.location = analyzer.location(tree);
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

	Facts.Statement linear(Tree source, ExpressionTree expression) {
        Facts.Statement result = base(Facts.STMT_LINEAR, source);
        result.expressions.add(analyzer.expression(expression));
        return result;
    }

}
