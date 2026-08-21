package dev.slopslap.structural;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

final class Facts {
    static final int EXPR_OTHER = 0;
    static final int EXPR_AND = 1;
    static final int EXPR_OR = 2;
    static final int EXPR_NOT = 3;
    static final int EXPR_CONDITIONAL = 4;

    static final int STMT_LINEAR = 0;
    static final int STMT_BLOCK = 1;
    static final int STMT_IF = 2;
    static final int STMT_LOOP = 3;
    static final int STMT_SWITCH = 4;
    static final int STMT_RETURN = 5;
    static final int STMT_PANIC = 6;
    static final int STMT_BREAK = 7;
    static final int STMT_CONTINUE = 8;

    record Location(String path, int line, int column, int endLine, int endColumn) { }

    static final class Expression {
        int kind = EXPR_OTHER;
        final List<Expression> children = new ArrayList<>();
        final List<String> calls = new ArrayList<>();
        final List<Function> nested = new ArrayList<>();
    }

    static final class CaseFact {
        boolean isDefault;
        boolean fallsThrough;
        final List<Expression> expressions = new ArrayList<>();
        final List<Statement> body = new ArrayList<>();
    }

    static final class Statement {
        int kind = STMT_LINEAR;
        Location location;
        Expression condition;
        final List<Expression> expressions = new ArrayList<>();
        final List<Statement> body = new ArrayList<>();
        final List<Statement> elseBody = new ArrayList<>();
        final List<CaseFact> cases = new ArrayList<>();
        boolean maySkip;
        boolean labeled;
    }

    static final class Function {
        String name;
        String receiver = "";
        String receiverVar = "this";
        Location location;
        final List<Statement> body = new ArrayList<>();
    }

    static final class TypeFact {
        String name;
        String kind;
        Location location;
        final List<Function> methods = new ArrayList<>();
        int interfaceMethodCount;
        final List<String> foreignTypes = new ArrayList<>();
        final Map<String, List<String>> methodFields = new LinkedHashMap<>();
        final List<String> foreignFields = new ArrayList<>();
        final List<String> fields = new ArrayList<>();
    }

    static final class Program {
        final List<Function> functions = new ArrayList<>();
        final List<TypeFact> types = new ArrayList<>();
        final List<String> files = new ArrayList<>();
    }

    private Facts() { }
}
