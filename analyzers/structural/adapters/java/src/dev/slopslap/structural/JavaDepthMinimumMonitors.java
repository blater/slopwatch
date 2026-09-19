package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.*;
import javax.lang.model.element.*;
import java.util.*;

/** Closed, declared-source monitor evidence; unknown or unguarded access rejects credit. */
final class JavaDepthMinimumMonitors {
    private final Trees trees;
    private final TypeElement owner;
    private final Map<String, Set<String>> accesses = new HashMap<>();
    private final Set<Element> stableLocks = new HashSet<>();
    private boolean exhausted;
    private int work;

    JavaDepthMinimumMonitors(Trees trees, TreePath ownerPath, TypeElement owner) {
        this.trees = trees;
        this.owner = owner;
        if (!(ownerPath.getLeaf() instanceof ClassTree declaration)) return;
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof VariableTree field) || !(field.getInitializer() instanceof NewClassTree allocation)) continue;
            TreePath path = new TreePath(ownerPath, field);
            Element element = trees.getElement(path);
            Element created = trees.getElement(TreePath.getPath(path, allocation));
            if (element instanceof VariableElement variable && variable.getModifiers().containsAll(Set.of(Modifier.PRIVATE, Modifier.FINAL))
                    && created instanceof ExecutableElement constructor && constructor.getEnclosingElement().toString().equals("java.lang.Object")) stableLocks.add(variable);
        }
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof MethodTree method) || method.getBody() == null) continue;
            TreePath path = new TreePath(ownerPath, method);
            if (!(trees.getElement(path) instanceof ExecutableElement executable) || executable.getKind() == ElementKind.CONSTRUCTOR) continue;
            new AccessScanner(methodMonitor(executable)).scan(new TreePath(path, method.getBody()), null);
        }
    }

    String methodMonitor(ExecutableElement method) {
        if (!method.getModifiers().contains(Modifier.SYNCHRONIZED)) return null;
        return owner.getQualifiedName() + (method.getModifiers().contains(Modifier.STATIC) ? "/class-monitor" : "/this-monitor");
    }

    String monitor(ExpressionTree expression, TreePath parent) {
        if (expression instanceof ParenthesizedTree p) return monitor(p.getExpression(), parent);
        if (expression instanceof IdentifierTree id && id.getName().contentEquals("this")) return owner.getQualifiedName() + "/this-monitor";
        TreePath path = TreePath.getPath(parent, expression);
        if (path == null) return null;
        if (expression instanceof MemberSelectTree select && select.getIdentifier().contentEquals("class")
                && owner.equals(trees.getElement(TreePath.getPath(path, select.getExpression())))) return owner.getQualifiedName() + "/class-monitor";
        Element element = trees.getElement(path);
        boolean own = expression instanceof IdentifierTree || expression instanceof MemberSelectTree member
                && member.getExpression() instanceof IdentifierTree id && id.getName().contentEquals("this");
        return own && stableLocks.contains(element) ? JavaDepthRoles.fieldID((VariableElement) element) + "/monitor" : null;
    }

    boolean protects(String field, String monitor) {
        return !exhausted && monitor != null && accesses.getOrDefault(field, Set.of()).equals(Set.of(monitor));
    }

    private final class AccessScanner extends TreePathScanner<Void, Void> {
        private String held;
        AccessScanner(String held) { this.held = held; }
        @Override public Void scan(Tree tree, Void unused) {
            if (++work > 8192) { exhausted = true; return null; }
            return super.scan(tree, unused);
        }
        @Override public Void visitSynchronized(SynchronizedTree tree, Void unused) {
            String previous = held;
            held = monitor(tree.getExpression(), getCurrentPath());
            scan(tree.getBlock(), unused);
            held = previous;
            return null;
        }
        @Override public Void visitIdentifier(IdentifierTree tree, Void unused) { record(); return null; }
        @Override public Void visitMemberSelect(MemberSelectTree tree, Void unused) { record(); return super.visitMemberSelect(tree, unused); }
        @Override public Void visitClass(ClassTree tree, Void unused) { return null; }
        @Override public Void visitLambdaExpression(LambdaExpressionTree tree, Void unused) { return null; }
        private void record() {
            Element element = trees.getElement(getCurrentPath());
            if (element instanceof VariableElement field && owner.equals(field.getEnclosingElement())
                    && field.getModifiers().contains(Modifier.PRIVATE)) {
                accesses.computeIfAbsent(JavaDepthRoles.fieldID(field), ignored -> new HashSet<>()).add(held == null ? "unguarded" : held);
            }
        }
    }
}
