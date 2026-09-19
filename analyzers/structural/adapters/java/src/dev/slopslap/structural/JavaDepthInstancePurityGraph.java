package dev.slopslap.structural;

import com.sun.source.tree.ClassTree;
import com.sun.source.tree.AssignmentTree;
import com.sun.source.tree.CompoundAssignmentTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.NewClassTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.ThrowTree;
import com.sun.source.tree.UnaryTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import java.util.IdentityHashMap;
import java.util.Map;

final class JavaDepthInstancePurityGraph {
    private JavaDepthInstancePurityGraph() { }

    static final class Graph {
        private static final int METHOD_LIMIT = 256;
        private final Trees trees;
        private final TreePath ownerPath;
        private final TypeElement owner;
        private final Map<ExecutableElement, TreePath> paths = new IdentityHashMap<>();
        private final Map<ExecutableElement, Boolean> results = new IdentityHashMap<>();
        private final Map<ExecutableElement, Boolean> visiting = new IdentityHashMap<>();

        Graph(Trees trees, TreePath ownerPath, TypeElement owner) {
            this.trees = trees;
            this.ownerPath = ownerPath;
            this.owner = owner;
            index();
        }

        boolean isStateless(ExecutableElement method) {
            if (!paths.containsKey(method) || paths.size() > METHOD_LIMIT) return false;
            Boolean result = results.get(method);
            if (result != null) return result;
            if (visiting.put(method, true) != null) return false;
            boolean valid = scan(method);
            visiting.remove(method);
            results.put(method, valid);
            return valid;
        }

        private void index() {
            if (!(ownerPath.getLeaf() instanceof ClassTree declaration)) return;
            for (Tree member : declaration.getMembers()) {
                if (!(member instanceof MethodTree methodTree)) continue;
                Element element = trees.getElement(new TreePath(ownerPath, methodTree));
                if (element instanceof ExecutableElement method && method.getKind() == ElementKind.METHOD) {
                    paths.put(method, new TreePath(ownerPath, methodTree));
                }
            }
        }

        private boolean scan(ExecutableElement method) {
            TreePath path = paths.get(method);
            if (!(path.getLeaf() instanceof MethodTree tree) || tree.getBody() == null || !dispatchable(method)) return false;
            PurityScanner scanner = new PurityScanner(method);
            scanner.scan(new TreePath(path, tree.getBody()), null);
            return scanner.valid;
        }

        private boolean dispatchable(ExecutableElement method) {
            return method.getModifiers().contains(Modifier.STATIC)
                    || owner.getModifiers().contains(Modifier.FINAL)
                    || method.getModifiers().contains(Modifier.PRIVATE)
                    || method.getModifiers().contains(Modifier.FINAL);
        }

        private boolean sameOwner(ExecutableElement method) {
            return method.getEnclosingElement() instanceof TypeElement type
                    && type.getQualifiedName().contentEquals(owner.getQualifiedName());
        }

        private final class PurityScanner extends TreePathScanner<Void, Void> {
            private final ExecutableElement current;
            boolean valid = true;

            PurityScanner(ExecutableElement current) { this.current = current; }

            @Override public Void visitNewClass(NewClassTree tree, Void unused) {
                if (!(getCurrentPath().getParentPath().getLeaf() instanceof ThrowTree)
                        || !standardArgumentError(tree)) valid = false;
                return super.visitNewClass(tree, unused);
            }

            private boolean standardArgumentError(NewClassTree tree) {
                Element element = trees.getElement(getCurrentPath());
                if (!(element instanceof ExecutableElement constructor)
                        || !(constructor.getEnclosingElement() instanceof TypeElement type)) return false;
                return type.getQualifiedName().contentEquals("java.lang.IllegalArgumentException")
                        && tree.getClassBody() == null;
            }

            @Override public Void visitMethodInvocation(MethodInvocationTree tree, Void unused) {
                Element element = trees.getElement(getCurrentPath());
                if (!(element instanceof ExecutableElement target)) {
                    valid = false;
                    return null;
                }
                if (!target.getModifiers().contains(Modifier.STATIC)
                        && (!sameOwner(target) || !JavaDepthCalls.instanceSelection(tree) || !isStateless(target))) {
                    valid = false;
                }
                for (ExpressionTree argument : tree.getArguments()) scan(argument, null);
                return null;
            }

            @Override public Void visitIdentifier(IdentifierTree tree, Void unused) {
                Element element = trees.getElement(getCurrentPath());
                if (tree.getName().contentEquals("this") || tree.getName().contentEquals("super")) {
                    if (!tree.getName().contentEquals("this") || !isFieldReceiver(getCurrentPath())) valid = false;
                }
                if (element instanceof VariableElement field && isReadableField(field)) return null;
                rejectMember(element);
                return super.visitIdentifier(tree, unused);
            }

            @Override public Void visitMemberSelect(MemberSelectTree tree, Void unused) {
                Element selected = trees.getElement(getCurrentPath());
                if (selected instanceof VariableElement field && isReadableField(field)
                        && tree.getExpression() instanceof IdentifierTree receiver
                        && receiver.getName().contentEquals("this")) return null;
                rejectMember(selected);
                return super.visitMemberSelect(tree, unused);
            }

            @Override public Void visitAssignment(AssignmentTree tree, Void unused) {
                if (isFieldTarget(tree.getVariable())) valid = false;
                return super.visitAssignment(tree, unused);
            }

            @Override public Void visitCompoundAssignment(CompoundAssignmentTree tree, Void unused) {
                if (isFieldTarget(tree.getVariable())) valid = false;
                return super.visitCompoundAssignment(tree, unused);
            }

            @Override public Void visitUnary(UnaryTree tree, Void unused) {
                if ((tree.getKind() == Tree.Kind.PREFIX_INCREMENT || tree.getKind() == Tree.Kind.PREFIX_DECREMENT
                        || tree.getKind() == Tree.Kind.POSTFIX_INCREMENT || tree.getKind() == Tree.Kind.POSTFIX_DECREMENT)
                        && isFieldTarget(tree.getExpression())) valid = false;
                return super.visitUnary(tree, unused);
            }

            private boolean isFieldReceiver(TreePath path) {
                return path.getParentPath() != null
                        && path.getParentPath().getLeaf() instanceof MemberSelectTree member
                        && member.getExpression() == path.getLeaf()
                        && trees.getElement(new TreePath(path.getParentPath(), member)) instanceof VariableElement field
                        && isReadableField(field);
            }

            private boolean isFieldTarget(ExpressionTree expression) {
                TreePath target = TreePath.getPath(getCurrentPath(), expression);
                Element element = target == null ? null : trees.getElement(target);
                return element instanceof VariableElement field && isReadableField(field);
            }

            private boolean isReadableField(VariableElement field) {
                return field.getEnclosingElement().equals(owner)
                        && field.getModifiers().contains(Modifier.PRIVATE)
                        && !field.getModifiers().contains(Modifier.STATIC)
                        && !field.getModifiers().contains(Modifier.VOLATILE)
                        && JavaDepthTypes.scalar(field.asType());
            }

            private void rejectMember(Element element) {
                if (element instanceof VariableElement variable
                        && variable.getEnclosingElement() == owner
                        && !variable.getModifiers().contains(Modifier.STATIC)) valid = false;
            }
        }
    }
}
