package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.SourcePositions;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;

import javax.tools.Diagnostic;
import java.util.*;

final class JavaAnalyzer extends TreePathScanner<Void, Void> {
    private static final Set<String> PRIMITIVES = Set.of(
            "boolean", "byte", "char", "double", "float", "int", "long", "short", "void"
    );

    private final Facts.Program program = new Facts.Program();
    private final Trees trees;
    private final SourcePositions positions;
    private CompilationUnitTree unit;
    private String path;

    JavaAnalyzer(Trees trees) {
        this.trees = trees;
        this.positions = trees.getSourcePositions();
    }

    void scanUnit(CompilationUnitTree current, String relative) {
        unit = current;
        path = relative;
        program.files.add(relative);
        scan(current, null);
    }

    void skipUnit(String relative) {
        program.files.add(relative);
    }

    Facts.Program program() {
        return program;
    }

    void order() {
        Comparator<Facts.Location> locations = Comparator.comparing(Facts.Location::path)
                .thenComparingInt(Facts.Location::line).thenComparingInt(Facts.Location::column);
        program.functions.sort(Comparator.comparing(item -> item.location, locations));
        program.types.sort(Comparator.comparing(item -> item.location, locations));
        program.files.sort(String::compareTo);
    }

    Facts.Location location(Tree tree) {
        long start = positions.getStartPosition(unit, tree);
        long end = positions.getEndPosition(unit, tree);
        if (start == Diagnostic.NOPOS) {
            start = 0;
        }
        if (end == Diagnostic.NOPOS || end < start) {
            end = start;
        }
        LineMap lines = unit.getLineMap();
        return new Facts.Location(
                path, (int) lines.getLineNumber(start), (int) lines.getColumnNumber(start),
                (int) lines.getLineNumber(end), (int) lines.getColumnNumber(end)
        );
    }

    @Override
    public Void visitClass(ClassTree node, Void unused) {
        String name = node.getSimpleName().toString();
        if (!name.isEmpty()) {
            Facts.TypeFact type = new Facts.TypeFact();
            type.name = name;
            type.kind = switch (node.getKind()) {
                case INTERFACE, ANNOTATION_TYPE -> "interface";
                case ENUM -> "enum";
                case RECORD -> "record";
                default -> "class";
            };
            type.location = location(node);
            Set<String> genericNames = new HashSet<>();
            node.getTypeParameters().forEach(value -> genericNames.add(value.getName().toString()));
            Set<String> foreignTypes = new TreeSet<>();
            if (node.getExtendsClause() != null) {
                collectTypes(node.getExtendsClause(), foreignTypes);
            }
            node.getImplementsClause().forEach(value -> collectTypes(value, foreignTypes));
            for (Tree member : node.getMembers()) {
                if (member instanceof VariableTree field) {
                    type.fields.add(field.getName().toString());
                    collectTypes(field.getType(), foreignTypes);
                }
            }
            int ordinal = 0;
            for (Tree member : node.getMembers()) {
                if (!(member instanceof MethodTree method)
                        || positions.getStartPosition(unit, method) == Diagnostic.NOPOS) {
                    continue;
                }
                Facts.Function function = function(method, name);
                program.functions.add(function);
                type.methods.add(function);
                if (type.kind.equals("interface")) {
                    type.interfaceMethodCount++;
                }
                String methodKey = method.getName() + "#" + ordinal++;
                FieldUses fields = new FieldUses(type.fields, method.getParameters());
                if (method.getBody() != null) {
                    fields.scan(method.getBody(), null);
                }
                type.methodFields.put(methodKey, new ArrayList<>(fields.own));
                type.foreignFields.addAll(fields.foreign);
                collectMethodTypes(method, foreignTypes);
                new BodyTypeNames(foreignTypes).scan(method.getBody(), null);
            }
            foreignTypes.remove(name);
            foreignTypes.removeAll(genericNames);
            foreignTypes.removeAll(PRIMITIVES);
            type.foreignTypes.addAll(foreignTypes);
            SortedSet<String> uniqueForeignFields = new TreeSet<>();
            type.foreignFields.stream().map(String::trim).forEach(uniqueForeignFields::add);
            type.foreignFields.clear();
            type.foreignFields.addAll(uniqueForeignFields);
            program.types.add(type);
        }
        return super.visitClass(node, unused);
    }

    private void collectMethodTypes(MethodTree method, Set<String> output) {
        if (method.getReturnType() != null) {
            collectTypes(method.getReturnType(), output);
        }
        method.getParameters().forEach(value -> collectTypes(value.getType(), output));
        method.getThrows().forEach(value -> collectTypes(value, output));
    }

    private void collectTypes(Tree tree, Set<String> output) {
        new TypeNames(output).scan(tree, null);
    }

    private Facts.Function function(MethodTree method, String owner) {
        Facts.Function result = new Facts.Function();
        result.name = method.getName().contentEquals("<init>") ? owner : method.getName().toString();
        result.receiver = owner;
        result.location = location(method);
        if (method.getBody() != null) {
            result.body.addAll(statements(method.getBody().getStatements()));
        }
        return result;
    }

    private List<Facts.Statement> statements(List<? extends StatementTree> input) {
        return new JavaStatements(this).translate(input);
    }

    Facts.Expression expression(ExpressionTree value) {
        if (value instanceof ParenthesizedTree parenthesized) {
            return expression(parenthesized.getExpression());
        }
        Facts.Expression result = new Facts.Expression();
        if (value instanceof BinaryTree binary
                && (value.getKind() == Tree.Kind.CONDITIONAL_AND
                    || value.getKind() == Tree.Kind.CONDITIONAL_OR)) {
            result.kind = value.getKind() == Tree.Kind.CONDITIONAL_AND
                    ? Facts.EXPR_AND : Facts.EXPR_OR;
            result.children.add(expression(binary.getLeftOperand()));
            result.children.add(expression(binary.getRightOperand()));
            return result;
        }
        if (value instanceof UnaryTree unary && value.getKind() == Tree.Kind.LOGICAL_COMPLEMENT) {
            result.kind = Facts.EXPR_NOT;
            result.children.add(expression(unary.getExpression()));
            return result;
        }
        if (value instanceof ConditionalExpressionTree conditional) {
            result.kind = Facts.EXPR_CONDITIONAL;
            result.children.add(expression(conditional.getCondition()));
            result.children.add(expression(conditional.getTrueExpression()));
            result.children.add(expression(conditional.getFalseExpression()));
            return result;
        }
        new ExpressionMetadata(result).scan(value, null);
        result.calls.sort(String::compareTo);
        return result;
    }

    private final class ExpressionMetadata extends TreeScanner<Void, Void> {
        private final Facts.Expression result;

        private ExpressionMetadata(Facts.Expression result) {
            this.result = result;
        }

        @Override
        public Void visitMethodInvocation(MethodInvocationTree node, Void unused) {
            if (node.getMethodSelect() instanceof IdentifierTree selected) {
                result.calls.add(selected.getName().toString());
            } else if (node.getMethodSelect() instanceof MemberSelectTree selected
                    && selected.getExpression().toString().equals("this")) {
                result.calls.add(selected.getIdentifier().toString());
            }
            return super.visitMethodInvocation(node, unused);
        }

        @Override
        public Void visitConditionalExpression(ConditionalExpressionTree node, Void unused) {
            result.children.add(expression(node));
            return null;
        }

        @Override
        public Void visitBinary(BinaryTree node, Void unused) {
            if (node.getKind() == Tree.Kind.CONDITIONAL_AND
                    || node.getKind() == Tree.Kind.CONDITIONAL_OR) {
                result.children.add(expression(node));
                return null;
            }
            return super.visitBinary(node, unused);
        }

        @Override
        public Void visitLambdaExpression(LambdaExpressionTree node, Void unused) {
            Facts.Function nested = new Facts.Function();
            nested.name = "<lambda>";
            nested.location = location(node);
            if (node.getBody() instanceof BlockTree block) {
                nested.body.addAll(statements(block.getStatements()));
            } else if (node.getBody() instanceof ExpressionTree body) {
                Facts.Statement statement = new Facts.Statement();
                statement.kind = Facts.STMT_LINEAR;
                statement.location = location(body);
                statement.expressions.add(expression(body));
                nested.body.add(statement);
            }
            result.nested.add(nested);
            return null;
        }
    }

    private static final class TypeNames extends TreeScanner<Void, Void> {
        private final Set<String> output;

        private TypeNames(Set<String> output) {
            this.output = output;
        }

        @Override
        public Void visitIdentifier(IdentifierTree node, Void unused) {
            output.add(node.getName().toString());
            return null;
        }

        @Override
        public Void visitMemberSelect(MemberSelectTree node, Void unused) {
            output.add(node.toString());
            return null;
        }
    }

    private static final class BodyTypeNames extends TreeScanner<Void, Void> {
        private final Set<String> output;

        private BodyTypeNames(Set<String> output) {
            this.output = output;
        }

        private void type(Tree value) {
            if (value != null) {
                new TypeNames(output).scan(value, null);
            }
        }

        @Override
        public Void visitVariable(VariableTree node, Void unused) {
            type(node.getType());
            return scan(node.getInitializer(), unused);
        }

        @Override
        public Void visitNewClass(NewClassTree node, Void unused) {
            type(node.getIdentifier());
            node.getTypeArguments().forEach(this::type);
            scan(node.getEnclosingExpression(), unused);
            node.getArguments().forEach(value -> scan(value, unused));
            return null;
        }

        @Override
        public Void visitNewArray(NewArrayTree node, Void unused) {
            type(node.getType());
            node.getDimensions().forEach(value -> scan(value, unused));
            if (node.getInitializers() != null) {
                node.getInitializers().forEach(value -> scan(value, unused));
            }
            return null;
        }

        @Override
        public Void visitTypeCast(TypeCastTree node, Void unused) {
            type(node.getType());
            return scan(node.getExpression(), unused);
        }

        @Override
        public Void visitInstanceOf(InstanceOfTree node, Void unused) {
            type(node.getType());
            scan(node.getExpression(), unused);
            return scan(node.getPattern(), unused);
        }

        @Override
        public Void visitAnnotation(AnnotationTree node, Void unused) {
            type(node.getAnnotationType());
            node.getArguments().forEach(value -> scan(value, unused));
            return null;
        }

        @Override
        public Void visitMethodInvocation(MethodInvocationTree node, Void unused) {
            node.getTypeArguments().forEach(this::type);
            return super.visitMethodInvocation(node, unused);
        }

        @Override
        public Void visitMemberReference(MemberReferenceTree node, Void unused) {
            if (node.getTypeArguments() != null) {
                node.getTypeArguments().forEach(this::type);
            }
            return super.visitMemberReference(node, unused);
        }
    }

    private static final class FieldUses extends TreeScanner<Void, Void> {
        private final Set<String> ownFields;
        private final Set<String> locals = new HashSet<>();
        private final SortedSet<String> own = new TreeSet<>();
        private final SortedSet<String> foreign = new TreeSet<>();

        private FieldUses(List<String> fields, List<? extends VariableTree> parameters) {
            ownFields = Set.copyOf(fields);
            parameters.forEach(value -> locals.add(value.getName().toString()));
        }

        @Override
        public Void visitVariable(VariableTree node, Void unused) {
            if (node.getInitializer() != null) {
                scan(node.getInitializer(), unused);
            }
            locals.add(node.getName().toString());
            return null;
        }

        @Override
        public Void visitIdentifier(IdentifierTree node, Void unused) {
            String name = node.getName().toString();
            if (ownFields.contains(name) && !locals.contains(name)) {
                own.add(name);
            }
            return null;
        }

        @Override
        public Void visitMemberSelect(MemberSelectTree node, Void unused) {
            String base = node.getExpression().toString();
            String field = node.getIdentifier().toString();
            if ((base.equals("this") || base.endsWith(".this")) && ownFields.contains(field)) {
                own.add(field);
            } else if (!base.equals("this")) {
                foreign.add(base + "." + field);
            }
            scan(node.getExpression(), unused);
            return null;
        }

        @Override
        public Void visitMethodInvocation(MethodInvocationTree node, Void unused) {
            if (node.getMethodSelect() instanceof MemberSelectTree selected) {
                scan(selected.getExpression(), unused);
            }
            node.getArguments().forEach(value -> scan(value, unused));
            return null;
        }

        @Override
        public Void visitMemberReference(MemberReferenceTree node, Void unused) {
            scan(node.getQualifierExpression(), unused);
            return null;
        }
    }
}
