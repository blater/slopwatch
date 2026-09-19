package dev.slopslap.structural;

import com.sun.source.tree.ClassTree;
import com.sun.source.tree.Tree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.NestingKind;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.ArrayList;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;

/** Resolved proof for a final immutable instance carrier with passive getters. */
final class JavaDepthCarrier {
    private JavaDepthCarrier() { }

    record Proof(List<ExecutableElement> constructors, List<ExecutableElement> accessors) {
        Proof {
            constructors = List.copyOf(constructors);
            accessors = List.copyOf(accessors);
        }
    }

    static Proof inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        if (!eligibleType(owner) || !(ownerPath.getLeaf() instanceof ClassTree declaration)) return null;
        Inspection inspection = new Inspection(trees, ownerPath, owner, declaration);
        return inspection.prove() ? inspection.proof() : null;
    }

    static boolean eligibleType(TypeElement owner) {
        if (owner == null || owner.getKind() != ElementKind.CLASS
                || !owner.getModifiers().contains(Modifier.FINAL)
                || owner.getNestingKind() != NestingKind.TOP_LEVEL
                || !owner.getTypeParameters().isEmpty() || !owner.getInterfaces().isEmpty()) return false;
        TypeMirror superclass = owner.getSuperclass();
        return superclass != null && (superclass.getKind() == TypeKind.NONE
                || "java.lang.Object".equals(superclass.toString()));
    }

    static final class Inspection {
        final Trees trees;
        final TreePath ownerPath;
        final TypeElement owner;
        final ClassTree declaration;
        final Map<Element, Tree> treesByElement = new IdentityHashMap<>();
        final List<VariableElement> fields = new ArrayList<>();
        final List<ExecutableElement> constructors = new ArrayList<>();
        final List<ExecutableElement> accessors = new ArrayList<>();
        boolean hasGuard;
        boolean hasVisibleGuard;

        Inspection(Trees trees, TreePath ownerPath, TypeElement owner, ClassTree declaration) {
            this.trees = trees;
            this.ownerPath = ownerPath;
            this.owner = owner;
            this.declaration = declaration;
        }

        boolean prove() {
            return JavaDepthCarrierMembers.index(this) && !fields.isEmpty()
                    && JavaDepthCarrierFields.check(this)
                    && JavaDepthCarrierConstructors.check(this)
                    && JavaDepthCarrierAccessors.check(this)
                    && JavaDepthCarrierMembers.visibleConstructor(this);
        }

        Proof proof() { return new Proof(constructors, accessors); }

        TreePath methodPath(ExecutableElement method) {
            Tree tree = treesByElement.get(method);
            return tree == null ? ownerPath : new TreePath(ownerPath, tree);
        }

        TypeMirror type(Tree tree, ExecutableElement context) {
            return type(tree, context == null ? ownerPath : methodPath(context));
        }

        TypeMirror type(Tree tree, TreePath root) {
            TreePath path = TreePath.getPath(root, tree);
            return path == null ? null : trees.getTypeMirror(path);
        }

        <T extends Element> T element(Tree tree, Class<T> type) {
            Element element = trees.getElement(new TreePath(ownerPath, tree));
            return type.isInstance(element) ? type.cast(element) : null;
        }
    }
}
