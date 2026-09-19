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

final class JavaDepthMinimumScannerExpressions {
    private final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerTraversal traversal;
    private final JavaDepthMinimumScannerServices services;
    private final Trees trees;
    private final JavaDepthComputationIdentity.Session computations;
    private final Set<String> limitations;
    private final JavaDepthMinimumBehavior.Summary result;
    private final TypeElement owner;
    private final Set<Element> connected;
    private final Map<Element, JavaDepthMinimumBehavior.ExprFacts> locals;
    private final boolean constructor;

    private Void scan(Tree tree, Void unused) { return traversal.scan(tree, unused); }
    private TreePath getCurrentPath() { return traversal.currentPath(); }

    JavaDepthMinimumScannerExpressions(JavaDepthMinimumScannerCore context,
            JavaDepthMinimumScannerTraversal traversal,
            JavaDepthMinimumScannerServices services) {
        this.context = context;
        this.traversal = traversal;
        this.services = services;
        this.trees = context.trees;
        this.computations = context.computations;
        this.limitations = context.limitations;
        this.result = context.result;
        this.owner = context.owner;
        this.connected = context.connected;
        this.locals = context.locals;
        this.constructor = context.constructor;
    }

    protected JavaDepthMinimumBehavior.ExprFacts expression(ExpressionTree tree, TreePath parent) {
        JavaDepthMinimumBehavior.ExprFacts facts = new JavaDepthMinimumBehavior.ExprFacts();
        TreePath path = TreePath.getPath(parent, tree);
        if (path == null) return facts;
        new ExpressionScanner(facts).scan(path, null);
        JavaDepthComputationIdentity.Result normalized = computations.assess(path, owner);
        if (normalized.identity() != null) {
            facts.reads.clear();
            facts.reads.addAll(normalized.stateReads());
            facts.connected = normalized.connected();
            facts.transform = normalized.transformed();
            facts.categories.remove("X");
        } else if (tree instanceof ConditionalExpressionTree || tree instanceof MethodInvocationTree) {
            // Unsupported branch/call syntax is not evidence of a returned transformation.
            facts.transform = false;
            facts.categories.remove("X");
            limitations.add(result.id + ": returned expression cannot be normalized; no transformation credit inferred");
        }
        return facts;
    }

    protected boolean containsThrow(Tree tree) {
        if (tree == null) return false;
        final boolean[] found = {false};
        TreePath path = TreePath.getPath(getCurrentPath(), tree);
        if (path == null) return false;
        new TreePathScanner<Void, Void>() {
            public Void visitThrow(ThrowTree node, Void unused) {
                if (recognizedError(node.getExpression(), getCurrentPath())) found[0] = true;
                return null;
            }
            public Void visitClass(ClassTree node, Void unused) { return null; }
            public Void visitLambdaExpression(LambdaExpressionTree node, Void unused) { return null; }
            public Void visitIf(IfTree node, Void unused) {
                Object value = JavaDepthMinimumExpressions.constant(node.getCondition(), getCurrentPath(), trees);
                if (value instanceof Boolean selected) {
                    scan(selected ? node.getThenStatement() : node.getElseStatement(), unused);
                    return null;
                }
                return super.visitIf(node, unused);
            }
        }.scan(path, null);
        return found[0];
    }

    protected boolean recognizedError(ExpressionTree expression, TreePath parent) {
        if (!(expression instanceof NewClassTree created)) return false;
        TreePath path = TreePath.getPath(parent, created);
        Element element = path == null ? null : trees.getElement(path);
        if (!(element instanceof ExecutableElement constructor)
                || constructor.getKind() != ElementKind.CONSTRUCTOR
                || !(constructor.getEnclosingElement() instanceof TypeElement type)
                || !type.getQualifiedName().contentEquals("java.lang.IllegalArgumentException")
                || !javaBase(type)) return false;
        List<? extends ExpressionTree> arguments = created.getArguments();
        if (arguments.isEmpty()) return true;
        if (arguments.size() != 1) return false;
        TreePath argumentPath = TreePath.getPath(parent, arguments.get(0));
        TypeMirror argumentType = argumentPath == null ? null : trees.getTypeMirror(argumentPath);
        if (argumentType == null || !argumentType.toString().equals("java.lang.String")) return false;
        ExpressionTree argument = arguments.get(0);
        if (argument instanceof LiteralTree literal) return literal.getValue() instanceof String;
        Element value = argumentPath == null ? null : trees.getElement(argumentPath);
        if (!(value instanceof VariableElement variable)
                || !(variable.getConstantValue() instanceof String)
                || !(argument instanceof IdentifierTree || argument instanceof MemberSelectTree member
                && typeElement(TreePath.getPath(parent, member.getExpression())))) return false;
        return true;
    }

    protected boolean typeElement(TreePath path) {
        return path != null && trees.getElement(path) instanceof TypeElement;
    }

    protected boolean javaBase(TypeElement type) {
        Element enclosing = type;
        while (enclosing != null && enclosing.getKind() != ElementKind.MODULE) {
            enclosing = enclosing.getEnclosingElement();
        }
        return enclosing instanceof ModuleElement module
                && module.getQualifiedName().contentEquals("java.base");
    }


    private void tick() { context.tick(); }


    protected final class ExpressionScanner extends TreePathScanner<Void, Void> {
        protected final JavaDepthMinimumBehavior.ExprFacts facts;
        ExpressionScanner(JavaDepthMinimumBehavior.ExprFacts facts) { this.facts = facts; }
        @Override public Void visitIdentifier(IdentifierTree tree, Void unused) {
            tick();
            Element element = trees.getElement(getCurrentPath());
            if (element != null && connected.contains(element)) facts.connected = true;
            if (element instanceof VariableElement field && owner.equals(field.getEnclosingElement())) {
                facts.reads.add(field);
            }
            if (element != null && locals.containsKey(element)) facts.merge(locals.get(element));
            return super.visitIdentifier(tree, unused);
        }
        @Override public Void visitMemberSelect(MemberSelectTree tree, Void unused) {
            tick();
            Element element = trees.getElement(getCurrentPath());
            if (element instanceof VariableElement field && owner.equals(field.getEnclosingElement())
                    && services.ownField(tree) && field.getConstantValue() == null) {
                if (!constructor) facts.connected = true;
                facts.reads.add(field);
            }
            return super.visitMemberSelect(tree, unused);
        }
        @Override public Void scan(Tree tree, Void unused) {
            if (tree == null) return null;
            tick();
            return super.scan(tree, unused);
        }
        @Override public Void visitBinary(BinaryTree tree, Void unused) {
            if (JavaDepthMinimumExpressions.constant(tree, getCurrentPath(), trees) != null) return null;
            ExpressionTree identity = JavaDepthMinimumExpressions.identity(tree, getCurrentPath(), trees);
            if (identity != null) {
                scan(identity, unused);
                return null;
            }
            if (JavaDepthMinimumBehavior.isTransform(tree.getKind())) facts.transform = true;
            return super.visitBinary(tree, unused);
        }
        @Override public Void visitConditionalExpression(ConditionalExpressionTree tree, Void unused) {
            // Branch syntax alone says nothing about the returned computation.
            normalizedReturn(getCurrentPath());
            return null;
        }
        @Override public Void visitArrayAccess(ArrayAccessTree tree, Void unused) {
            facts.transform = true;
            return super.visitArrayAccess(tree, unused);
        }
        @Override public Void visitMethodInvocation(MethodInvocationTree tree, Void unused) {
            tick();
            // Substitute actual arguments into the exact helper return before
            // deciding whether its result depends on caller input.
            normalizedReturn(getCurrentPath());
            return null;
        }

        protected void normalizedReturn(TreePath path) {
            JavaDepthComputationIdentity.Result normalized = computations.assess(path, owner);
            if (normalized.identity() == null) return;
            facts.reads.addAll(normalized.stateReads());
            facts.connected |= normalized.connected();
            facts.transform |= normalized.transformed();
        }

        protected boolean exactReceiver(MethodInvocationTree tree) {
            ExpressionTree select = tree.getMethodSelect();
            if (select instanceof IdentifierTree) return true;
            return select instanceof MemberSelectTree member && JavaDepthMinimumBehavior.isThis(member.getExpression());
        }
    }
}
