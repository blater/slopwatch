package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.TypeMirror;
import java.util.*;

/** Canonical structural identity for a returned bounded computation. */
final class JavaDepthComputationIdentity {
    private static final int MAX_NODES = 256;
    private static final int MAX_INDEX_NODES = 1024;
    private static final int MAX_HELPERS = 32;

    public record Result(String identity, boolean connected, boolean transformed, Set<VariableElement> stateReads) { }

    public static String of(Trees trees, TreePath expressionPath, TypeElement owner) {
        return assess(trees, expressionPath, owner).identity();
    }

    public static Result assess(Trees trees, TreePath expressionPath, TypeElement owner) {
        if (trees == null || expressionPath == null || owner == null
                || !(expressionPath.getLeaf() instanceof ExpressionTree)) return new Result(null, false, false, Set.of());
        TreePath methodPath = enclosing(expressionPath, MethodTree.class);
        TreePath ownerPath = enclosing(expressionPath, ClassTree.class);
        if (methodPath == null || ownerPath == null) return new Result(null, false, false, Set.of());
        try {
            return new Canonicalizer(trees, owner, ownerPath, methodPath).result(expressionPath);
        } catch (Limit ignored) {
            return new Result(null, false, false, Set.of());
        }
    }

    /** Per-boundary cache: source trees are immutable during one attributed analysis. */
    static final class Session {
        private final Trees trees;
        private final Map<Tree, Canonicalizer> methods = new IdentityHashMap<>();
        private final Set<Tree> unsupported = Collections.newSetFromMap(new IdentityHashMap<>());
        private final Map<Tree, Result> results = new IdentityHashMap<>();

        Session(Trees trees) { this.trees = trees; }

        Result assess(TreePath expressionPath, TypeElement owner) {
            if (expressionPath == null) return new Result(null, false, false, Set.of());
            Result known = results.get(expressionPath.getLeaf());
            if (known != null) return known;
            TreePath methodPath = enclosing(expressionPath, MethodTree.class);
            TreePath ownerPath = enclosing(expressionPath, ClassTree.class);
            Result result = new Result(null, false, false, Set.of());
            if (methodPath != null && ownerPath != null && !unsupported.contains(methodPath.getLeaf())) {
                try {
                    Canonicalizer method = methods.get(methodPath.getLeaf());
                    if (method == null) {
                        method = new Canonicalizer(trees, owner, ownerPath, methodPath);
                        methods.put(methodPath.getLeaf(), method);
                    }
                    result = method.result(expressionPath);
                } catch (Limit ignored) {
                    unsupported.add(methodPath.getLeaf());
                }
            }
            results.put(expressionPath.getLeaf(), result);
            return result;
        }
    }

    public static boolean meaningful(String identity) {
        return identity != null && identity.startsWith("op[dyn](");
    }

    private static TreePath enclosing(TreePath path, Class<?> kind) {
        for (TreePath current = path; current != null; current = current.getParentPath()) {
            if (kind.isInstance(current.getLeaf())) return current;
        }
        return null;
    }

    private static final class Canonicalizer {
        private final Trees trees;
        private final TypeElement owner;
        private final TreePath ownerPath;
        private final TreePath methodPath;
        private final Map<Element, Binding> locals = new IdentityHashMap<>();
        private final Map<ExecutableElement, TreePath> methodPaths = new IdentityHashMap<>();
        private final Set<ExecutableElement> activeHelpers = Collections.newSetFromMap(new IdentityHashMap<>());
        private final Map<VariableElement, Node> substitutions = new IdentityHashMap<>();
        private final Set<VariableElement> assigned = Collections.newSetFromMap(new IdentityHashMap<>());
        private int nodes;
        private int indexNodes;
        private int helpers;

        Canonicalizer(Trees trees, TypeElement owner, TreePath ownerPath, TreePath methodPath) {
            this.trees = trees;
            this.owner = owner;
            this.ownerPath = ownerPath;
            this.methodPath = methodPath;
            indexMethods();
            indexLocals();
        }

        Result result(TreePath path) {
            nodes = 0;
            helpers = 0;
            try {
                Node node = canonical(path);
                return node == null ? new Result(null, false, false, Set.of())
                        : new Result(node.text, node.connected, node.transformed, node.stateReads);
            } catch (Limit ignored) {
                return new Result(null, false, false, Set.of());
            }
        }

        private Node canonical(TreePath path) {
            if (path == null || ++nodes > MAX_NODES) throw new Limit();
            Tree tree = path.getLeaf();
            if (tree instanceof ParenthesizedTree parenthesized) return canonical(child(path, parenthesized.getExpression()));
            if (tree instanceof LiteralTree literal) return literal(literal, path);
            if (tree instanceof IdentifierTree identifier) return variable(path, identifier);
            if (tree instanceof MemberSelectTree member) return member(path, member);
            if (tree instanceof BinaryTree binary) return binary(path, binary);
            if (tree instanceof UnaryTree unary) return unary(path, unary);
            if (tree instanceof ConditionalExpressionTree conditional) {
                return conditional(path, conditional);
            }
            if (tree instanceof ArrayAccessTree array) {
                return operator(path, "index", List.of(array.getExpression(), array.getIndex()));
            }
            if (tree instanceof MethodInvocationTree invocation) return invocation(path, invocation);
            return null;
        }

        private Node binary(TreePath path, BinaryTree tree) {
            Object constant = JavaDepthMinimumExpressions.constant(tree, path.getParentPath(), trees);
            String type = type(path);
            if (constant != null && type != null) return node("literal(" + type + ":" + value(constant) + ")", false, false, constant);
            ExpressionTree identity = JavaDepthMinimumExpressions.identity(tree, path.getParentPath(), trees);
            if (identity != null) return canonical(child(path, identity));
            return operator(path, tree.getKind().name(), List.of(tree.getLeftOperand(), tree.getRightOperand()));
        }

        private Node unary(TreePath path, UnaryTree tree) {
            return operator(path, tree.getKind().name(), List.of(tree.getExpression()));
        }

        private Node conditional(TreePath path, ConditionalExpressionTree tree) {
            Node condition = canonical(child(path, tree.getCondition()));
            if (condition != null && condition.constant instanceof Boolean value) {
                return canonical(child(path, value ? tree.getTrueExpression() : tree.getFalseExpression()));
            }
            Node trueValue = canonical(child(path, tree.getTrueExpression()));
            Node falseValue = canonical(child(path, tree.getFalseExpression()));
            if (trueValue == null || falseValue == null) return null;
            if (trueValue.text.equals(falseValue.text)) return trueValue;
            if (condition == null) return null;
            return operator(path, "conditional", List.of(tree.getCondition(),
                    tree.getTrueExpression(), tree.getFalseExpression()));
        }

        private Node operator(TreePath path, String operation, List<? extends Tree> operands) {
            String type = type(path);
            if (type == null) return null;
            List<Node> nodes = new ArrayList<>();
            for (Tree operand : operands) {
                Node value = canonical(child(path, operand));
                if (value == null) return null;
                nodes.add(value);
            }
            boolean connected = nodes.stream().anyMatch(node -> node.connected);
            boolean transformed = connected;
            Node identity = identity(operation, nodes, type);
            if (identity != null) return identity;
            Object constant = constant(operation, nodes);
            if (constant != null) return node("literal(" + type + ":" + value(constant) + ")", false, false, constant);
            List<String> values = nodes.stream().map(node -> node.text).toList();
            String prefix = connected ? "op[dyn](" : "op[const](";
            Set<VariableElement> stateReads = new HashSet<>();
            for (Node operand : nodes) stateReads.addAll(operand.stateReads);
            return new Node(prefix + operation + ":" + type + ";" + String.join(",", values) + ")",
                    connected, transformed, null, Set.copyOf(stateReads));
        }

        private Node identity(String operation, List<Node> nodes, String type) {
            if (nodes.size() != 2) return null;
            Node left = nodes.get(0), right = nodes.get(1);
            if (operation.equals("PLUS") && numeric(type) && zero(right)) return left;
            if ((operation.equals("MINUS") || operation.equals("LEFT_SHIFT") || operation.equals("RIGHT_SHIFT")
                    || operation.equals("UNSIGNED_RIGHT_SHIFT") || operation.equals("OR") || operation.equals("XOR"))
                    && zero(right)) return left;
            if (operation.equals("MULTIPLY") && one(right)) return left;
            if (operation.equals("MULTIPLY") && one(left)) return right;
            if (operation.equals("DIVIDE") && one(right)) return left;
            if (operation.equals("CONDITIONAL_AND") && Boolean.TRUE.equals(right.constant)) return left;
            if (operation.equals("CONDITIONAL_AND") && Boolean.TRUE.equals(left.constant)) return right;
            if (operation.equals("CONDITIONAL_OR") && Boolean.FALSE.equals(right.constant)) return left;
            if (operation.equals("CONDITIONAL_OR") && Boolean.FALSE.equals(left.constant)) return right;
            return null;
        }

        private boolean numeric(String type) {
            return switch (type) {
                case "byte", "short", "int", "long", "char", "float", "double" -> true;
                default -> false;
            };
        }

        private Object constant(String operation, List<Node> nodes) {
            if (nodes.size() != 2 || nodes.get(0).constant == null || nodes.get(1).constant == null) return null;
            Object left = nodes.get(0).constant, right = nodes.get(1).constant;
            if (left instanceof Boolean l && right instanceof Boolean r) {
                if (operation.equals("CONDITIONAL_AND")) return l && r;
                if (operation.equals("CONDITIONAL_OR")) return l || r;
                if (operation.equals("EQUAL_TO")) return l == r;
                if (operation.equals("NOT_EQUAL_TO")) return l != r;
            }
            if (left instanceof Number l && right instanceof Number r) {
                double a = l.doubleValue(), b = r.doubleValue();
                return switch (operation) {
                    case "EQUAL_TO" -> a == b;
                    case "NOT_EQUAL_TO" -> a != b;
                    case "LESS_THAN" -> a < b;
                    case "LESS_THAN_EQUAL" -> a <= b;
                    case "GREATER_THAN" -> a > b;
                    case "GREATER_THAN_EQUAL" -> a >= b;
                    default -> null;
                };
            }
            return null;
        }

        private boolean zero(Node node) { return node.constant instanceof Number number && number.doubleValue() == 0; }
        private boolean one(Node node) { return node.constant instanceof Number number && number.doubleValue() == 1; }

        private Node member(TreePath path, MemberSelectTree tree) {
            Element element = trees.getElement(path);
            if (!(element instanceof VariableElement field)) return null;
            if (field.getConstantValue() != null && field.getModifiers().contains(Modifier.STATIC)
                    && typeReceiver(path, tree.getExpression(), field.getEnclosingElement())) return fieldNode(field);
            if (!owner.equals(field.getEnclosingElement()) || field.getConstantValue() == null && !ownedReceiver(tree.getExpression())) return null;
            return fieldNode(field);
        }

        private Node variable(TreePath path, IdentifierTree tree) {
            Element element = trees.getElement(path);
            if (!(element instanceof VariableElement variable)) return null;
            Node replacement = substitutions.get(variable);
            if (replacement != null) return replacement;
            if (variable.getConstantValue() != null && variable.getModifiers().contains(Modifier.STATIC)) return fieldNode(variable);
            if (owner.equals(variable.getEnclosingElement())) {
                return fieldNode(variable);
            }
            if (assigned.contains(variable)) return null;
            if (variable.getKind() == ElementKind.PARAMETER) return node(parameter(variable), true, false);
            Binding binding = locals.get(variable);
            if (binding == null || binding.initializer == null || !binding.assigned.isEmpty()
                    || binding.active) return null;
            binding.active = true;
            try {
                return canonical(binding.initializer);
            } finally {
                binding.active = false;
            }
        }

        private Node invocation(TreePath path, MethodInvocationTree tree) {
            Element element = trees.getElement(path);
            if (!(element instanceof ExecutableElement method) || !sourceHelper(method, path, tree.getMethodSelect())) return null;
            if (++helpers > MAX_HELPERS || activeHelpers.contains(method)) throw new Limit();
            TreePath targetPath = methodPaths.get(method);
            if (targetPath == null && method.getModifiers().contains(Modifier.STATIC)) targetPath = trees.getPath(method);
            if (targetPath == null || !(targetPath.getLeaf() instanceof MethodTree target)
                    || target.getBody() == null || target.getBody().getStatements().isEmpty()) return null;
            List<? extends StatementTree> statements = target.getBody().getStatements();
            if (!(statements.get(statements.size() - 1) instanceof ReturnTree returned)
                    || returned.getExpression() == null) return null;
            if (tree.getArguments().size() != method.getParameters().size()) return null;
            List<Node> arguments = new ArrayList<>();
            for (ExpressionTree argument : tree.getArguments()) {
                Node value = canonical(child(path, argument));
                if (value == null) return null;
                arguments.add(value);
            }
            Map<VariableElement, TreePath> helperInitializers = new IdentityHashMap<>();
            for (int index = 0; index < statements.size() - 1; index++) {
                if (!(statements.get(index) instanceof VariableTree variable)
                        || variable.getInitializer() == null) return null;
                TreePath variablePath = TreePath.getPath(targetPath, variable);
                Element localElement = variablePath == null ? null : trees.getElement(variablePath);
                if (!(localElement instanceof VariableElement local)) return null;
                TreePath initializer = TreePath.getPath(variablePath, variable.getInitializer());
                if (initializer == null) return null;
                helperInitializers.put(local, initializer);
            }
            Map<VariableElement, Node> previous = new IdentityHashMap<>();
            for (int index = 0; index < arguments.size(); index++) {
                VariableElement parameter = method.getParameters().get(index);
                previous.put(parameter, substitutions.put(parameter, arguments.get(index)));
            }
            Map<VariableElement, Binding> previousLocals = new IdentityHashMap<>();
            for (Map.Entry<VariableElement, TreePath> entry : helperInitializers.entrySet()) {
                previousLocals.put(entry.getKey(), locals.put(entry.getKey(), new Binding(entry.getValue())));
            }
            activeHelpers.add(method);
            Node result;
            try {
                result = canonical(TreePath.getPath(targetPath, returned.getExpression()));
            } finally {
                activeHelpers.remove(method);
                for (Map.Entry<VariableElement, Binding> entry : previousLocals.entrySet()) {
                    if (entry.getValue() == null) locals.remove(entry.getKey());
                    else locals.put(entry.getKey(), entry.getValue());
                }
                for (Map.Entry<VariableElement, Node> entry : previous.entrySet()) {
                    if (entry.getValue() == null) substitutions.remove(entry.getKey());
                    else substitutions.put(entry.getKey(), entry.getValue());
                }
            }
            return result;
        }

        private Node node(String text, boolean connected, boolean transformed) {
            return node(text, connected, transformed, null);
        }

        private Node node(String text, boolean connected, boolean transformed, Object constant) {
            return text == null ? null : new Node(text, connected, transformed, constant, Set.of());
        }

        private Node fieldNode(VariableElement field) {
            boolean state = field.getConstantValue() == null;
            return new Node(field(field), state, false, field.getConstantValue(), state ? Set.of(field) : Set.of());
        }

        private String parameter(VariableElement parameter) {
            Element enclosing = parameter.getEnclosingElement();
            if (!(enclosing instanceof ExecutableElement method)) return null;
            int index = method.getParameters().indexOf(parameter);
            return index < 0 ? null : "param(" + index + ":" + type(parameter.asType()) + ")";
        }

        private String field(VariableElement field) {
            return "field(" + ((TypeElement) field.getEnclosingElement()).getQualifiedName()
                    + "#" + field.getSimpleName() + ":" + type(field.asType()) + ")";
        }

        private Node literal(LiteralTree literal, TreePath path) {
            String type = type(path);
            if (type == null) return null;
            Object value = literal.getValue();
            return node("literal(" + type + ":" + value(value) + ")", false, false, value);
        }

        private String type(TypeMirror mirror) {
            return mirror == null ? null : mirror.toString();
        }

        private String type(TreePath path) { return type(trees.getTypeMirror(path)); }

        private boolean ownedReceiver(Tree expression) {
            if (expression instanceof IdentifierTree identifier) return identifier.getName().contentEquals("this");
            return false;
        }

        private boolean helperReceiver(ExpressionTree select) {
            if (select instanceof IdentifierTree) return true;
            return select instanceof MemberSelectTree member && ownedReceiver(member.getExpression());
        }

        private boolean sourceHelper(ExecutableElement method, TreePath invocationPath, ExpressionTree select) {
            if (!method.getModifiers().contains(Modifier.STATIC)) {
                return owner.equals(method.getEnclosingElement())
                        && method.getModifiers().contains(Modifier.PRIVATE) && helperReceiver(select);
            }
            TreePath targetPath = trees.getPath(method);
            if (targetPath == null || !(method.getEnclosingElement() instanceof TypeElement targetOwner)) return false;
            if (select instanceof IdentifierTree) return true;
            if (!(select instanceof MemberSelectTree member)) return false;
            TreePath receiverPath = TreePath.getPath(invocationPath, member.getExpression());
            Element receiver = receiverPath == null ? null : trees.getElement(receiverPath);
            return receiver != null && receiver.equals(targetOwner);
        }

        private boolean typeReceiver(TreePath path, Tree expression, Element expected) {
            TreePath receiverPath = TreePath.getPath(path, expression);
            Element receiver = receiverPath == null ? null : trees.getElement(receiverPath);
            return receiver != null && receiver.equals(expected);
        }

        private TreePath child(TreePath parent, Tree child) {
            if (parent == null || child == null) return null;
            TreePath path = TreePath.getPath(parent, child);
            return path == null ? new TreePath(parent, child) : path;
        }

        private void indexMethods() {
            if (!(ownerPath.getLeaf() instanceof ClassTree declaration)) return;
            for (Tree member : declaration.getMembers()) {
                if (!(member instanceof MethodTree method)) continue;
                TreePath path = new TreePath(ownerPath, method);
                Element element = trees.getElement(path);
                if (element instanceof ExecutableElement executable && owner.equals(executable.getEnclosingElement())) {
                    methodPaths.put(executable, path);
                }
            }
        }

        private void indexLocals() {
            new TreePathScanner<Void, Void>() {
                @Override public Void scan(Tree tree, Void unused) {
                    if (tree != null && ++indexNodes > MAX_INDEX_NODES) throw new Limit();
                    return super.scan(tree, unused);
                }

                @Override public Void visitVariable(VariableTree tree, Void unused) {
                    Element element = trees.getElement(getCurrentPath());
                    if (element instanceof VariableElement variable
                            && variable.getKind() == ElementKind.LOCAL_VARIABLE) {
                        TreePath initializer = tree.getInitializer() == null ? null
                                : TreePath.getPath(getCurrentPath(), tree.getInitializer());
                        locals.put(variable, new Binding(initializer));
                    }
                    return super.visitVariable(tree, unused);
                }

                @Override public Void visitAssignment(AssignmentTree tree, Void unused) {
                    markAssigned(tree.getVariable());
                    return super.visitAssignment(tree, unused);
                }

                @Override public Void visitCompoundAssignment(CompoundAssignmentTree tree, Void unused) {
                    markAssigned(tree.getVariable());
                    return super.visitCompoundAssignment(tree, unused);
                }

                @Override public Void visitUnary(UnaryTree tree, Void unused) {
                    if (tree.getKind() == Tree.Kind.PREFIX_INCREMENT || tree.getKind() == Tree.Kind.PREFIX_DECREMENT
                            || tree.getKind() == Tree.Kind.POSTFIX_INCREMENT || tree.getKind() == Tree.Kind.POSTFIX_DECREMENT) {
                        markAssigned(tree.getExpression());
                    }
                    return super.visitUnary(tree, unused);
                }

                private void markAssigned(Tree tree) {
                    Element element = trees.getElement(TreePath.getPath(getCurrentPath(), tree));
                    if (element instanceof VariableElement variable) {
                        assigned.add(variable);
                        Binding binding = locals.get(variable);
                        if (binding != null) binding.assigned.add(Boolean.TRUE);
                    }
                }
            }.scan(new TreePath(methodPath, ((MethodTree) methodPath.getLeaf()).getBody()), null);
        }
    }

    private record Node(String text, boolean connected, boolean transformed, Object constant, Set<VariableElement> stateReads) { }

    private static final class Binding {
        final TreePath initializer;
        final Set<Boolean> assigned = new HashSet<>();
        boolean active;
        Binding(TreePath initializer) { this.initializer = initializer; }
    }

    private static String value(Object value) {
        if (value == null) return "null";
        if (value instanceof String string) return '"' + string.replace("\\", "\\\\").replace("\"", "\\\"") + '"';
        if (value instanceof Character character) return "'" + character + "'";
        return String.valueOf(value);
    }

    private static final class Limit extends RuntimeException { }
    private JavaDepthComputationIdentity() { }
}
