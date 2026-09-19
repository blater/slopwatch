package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.TypeMirror;
import java.util.*;

/** Mutable per-method state shared by the computation collaborators. */
final class JavaDepthComputationState {
    static final int MAX_NODES = 256, MAX_INDEX_NODES = 1024, MAX_HELPERS = 32;
    final Trees trees;
    final TypeElement owner;
    final TreePath ownerPath;
    final TreePath methodPath;
    final Map<Element, Binding> locals = new IdentityHashMap<>();
    final Map<ExecutableElement, TreePath> methodPaths = new IdentityHashMap<>();
    final Set<ExecutableElement> activeHelpers = Collections.newSetFromMap(new IdentityHashMap<>());
    final Map<VariableElement, Node> substitutions = new IdentityHashMap<>();
    final Set<VariableElement> assigned = Collections.newSetFromMap(new IdentityHashMap<>());
    int nodes;
    int indexNodes;
    int helpers;

    JavaDepthComputationState(Trees trees, TypeElement owner, TreePath ownerPath, TreePath methodPath) {
        this.trees = trees; this.owner = owner; this.ownerPath = ownerPath; this.methodPath = methodPath;
        indexMethods(); indexLocals();
    }
    Node node(String text, boolean connected, boolean transformed) { return node(text, connected, transformed, null); }
    Node node(String text, boolean connected, boolean transformed, Object constant) {
        return text == null ? null : new Node(text, connected, transformed, constant, Set.of());
    }
    Node fieldNode(VariableElement field) {
        boolean state = field.getConstantValue() == null;
        return new Node(field(field), state, false, field.getConstantValue(), state ? Set.of(field) : Set.of());
    }
    String parameter(VariableElement parameter) {
        Element enclosing = parameter.getEnclosingElement();
        if (!(enclosing instanceof ExecutableElement method)) return null;
        int index = method.getParameters().indexOf(parameter);
        return index < 0 ? null : "param(" + index + ":" + type(parameter.asType()) + ")";
    }
    String field(VariableElement field) {
        return "field(" + ((TypeElement) field.getEnclosingElement()).getQualifiedName()
                + "#" + field.getSimpleName() + ":" + type(field.asType()) + ")";
    }
    Node literal(LiteralTree literal, TreePath path) {
        String type = type(path); if (type == null) return null;
        Object value = literal.getValue(); return node("literal(" + type + ":" + value(value) + ")", false, false, value);
    }
    String type(TypeMirror mirror) { return mirror == null ? null : mirror.toString(); }
    String type(TreePath path) { return type(trees.getTypeMirror(path)); }
    boolean ownedReceiver(Tree expression) { return expression instanceof IdentifierTree identifier && identifier.getName().contentEquals("this"); }
    boolean helperReceiver(ExpressionTree select) {
        return select instanceof IdentifierTree || select instanceof MemberSelectTree member && ownedReceiver(member.getExpression());
    }
    boolean typeReceiver(TreePath path, Tree expression, Element expected) {
        TreePath receiverPath = TreePath.getPath(path, expression);
        Element receiver = receiverPath == null ? null : trees.getElement(receiverPath);
        return receiver != null && receiver.equals(expected);
    }
    TreePath child(TreePath parent, Tree child) {
        if (parent == null || child == null) return null;
        TreePath path = TreePath.getPath(parent, child);
        return path == null ? new TreePath(parent, child) : path;
    }
    private void indexMethods() {
        if (!(ownerPath.getLeaf() instanceof ClassTree declaration)) return;
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof MethodTree method)) continue;
            TreePath path = new TreePath(ownerPath, method); Element element = trees.getElement(path);
            if (element instanceof ExecutableElement executable && owner.equals(executable.getEnclosingElement())) methodPaths.put(executable, path);
        }
    }
    private void indexLocals() {
        new TreePathScanner<Void, Void>() {
            @Override public Void scan(Tree tree, Void unused) {
                if (tree != null && ++indexNodes > MAX_INDEX_NODES) throw new JavaDepthComputationSupport.Limit();
                return super.scan(tree, unused);
            }
            @Override public Void visitVariable(VariableTree tree, Void unused) {
                Element element = trees.getElement(getCurrentPath());
                if (element instanceof VariableElement variable && variable.getKind() == ElementKind.LOCAL_VARIABLE) {
                    TreePath initializer = tree.getInitializer() == null ? null : TreePath.getPath(getCurrentPath(), tree.getInitializer());
                    locals.put(variable, new Binding(initializer));
                }
                return super.visitVariable(tree, unused);
            }
            @Override public Void visitAssignment(AssignmentTree tree, Void unused) { markAssigned(tree.getVariable()); return super.visitAssignment(tree, unused); }
            @Override public Void visitCompoundAssignment(CompoundAssignmentTree tree, Void unused) { markAssigned(tree.getVariable()); return super.visitCompoundAssignment(tree, unused); }
            @Override public Void visitUnary(UnaryTree tree, Void unused) {
                if (tree.getKind() == Tree.Kind.PREFIX_INCREMENT || tree.getKind() == Tree.Kind.PREFIX_DECREMENT
                        || tree.getKind() == Tree.Kind.POSTFIX_INCREMENT || tree.getKind() == Tree.Kind.POSTFIX_DECREMENT) markAssigned(tree.getExpression());
                return super.visitUnary(tree, unused);
            }
            private void markAssigned(Tree tree) {
                Element element = trees.getElement(TreePath.getPath(getCurrentPath(), tree));
                if (element instanceof VariableElement variable) { assigned.add(variable); Binding binding = locals.get(variable); if (binding != null) binding.assigned.add(Boolean.TRUE); }
            }
        }.scan(new TreePath(methodPath, ((MethodTree) methodPath.getLeaf()).getBody()), null);
    }
    static String value(Object value) {
        if (value == null) return "null";
        if (value instanceof String string) return '"' + string.replace("\\", "\\\\").replace("\"", "\\\"") + '"';
        if (value instanceof Character character) return "'" + character + "'";
        return String.valueOf(value);
    }
    record Node(String text, boolean connected, boolean transformed, Object constant, Set<VariableElement> stateReads) { }
    static final class Binding {
        final TreePath initializer; final Set<Boolean> assigned = new HashSet<>(); boolean active;
        Binding(TreePath initializer) { this.initializer = initializer; }
    }
}
