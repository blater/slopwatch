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

final class JavaDepthMinimumScannerCallEffects {
    private final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerTraversal traversal;
    private final JavaDepthMinimumScannerServices services;
    private final Trees trees;
    private final Set<String> sourceDelegations;
    private final JavaDepthMinimumBehavior.Summary result;
    private final TypeElement owner;
    private final Set<String> limitations;
    private final JavaDepthMinimumScannerCallAdmission admission;

    JavaDepthMinimumScannerCallEffects(JavaDepthMinimumScannerCore context,
            JavaDepthMinimumScannerTraversal traversal,
            JavaDepthMinimumScannerServices services,
            JavaDepthMinimumScannerCallAdmission admission) {
        this.context = context;
        this.traversal = traversal;
        this.services = services;
        this.admission = admission;
        this.trees = context.trees;
        this.sourceDelegations = context.sourceDelegations;
        this.result = context.result;
        this.owner = context.owner;
        this.limitations = context.limitations;
    }
    private Void scan(Tree tree, Void unused) { return traversal.scan(tree, unused); }
    private TreePath getCurrentPath() { return traversal.currentPath(); }

    public Void visitMethodInvocation(MethodInvocationTree tree, Void unused) {
        context.tick();
        traversal.scan(tree.getMethodSelect(), unused);
        traversal.scan(tree.getArguments(), unused);
        if (context.paths.stream().noneMatch(path -> path.live)) return null;
        Element target = trees.getElement(TreePath.getPath(getCurrentPath(), tree));
        if (target instanceof ExecutableElement called && admission.sourceDelegate(tree, called)) {
            sourceDelegations.add(JavaDepthMinimumBehavior.callableID(called));
            JavaDepthMinimumBehavior.Summary helper = context.host.summary(called);
            String receiverRoot = admission.delegateReceiverRoot(tree, called);
            boolean connectedInputs = !tree.getArguments().isEmpty();
            for (ExpressionTree argument : tree.getArguments()) {
                connectedInputs &= services.expression(argument, getCurrentPath()).connected;
            }
            List<Set<String>> effects = new ArrayList<>();
            for (Set<String> alternative : helper.alternatives) {
                Set<String> selected = new TreeSet<>();
                for (String item : alternative) {
                    int separator = item.indexOf('\u0000');
                    String category = item.substring(0, separator), root = item.substring(separator + 1);
                    if (!helper.effectCategories.contains(category)) continue;
                    if (category.equals("X") && !helper.effectTransformationRoots.contains(root)) continue;
                    // A helper's validation does not validate caller input when
                    // the call supplied constants or unresolved argument values.
                    if (category.equals("V") && !connectedInputs) continue;
                    String boundRoot = receiverRoot + root;
                    selected.add(category + "\u0000" + boundRoot);
                    context.result.credit(category, boundRoot);
                    result.effectCategories.add(category);
                    if (category.equals("X")) {
                        result.transformationRoots.add(boundRoot);
                        result.effectTransformationRoots.add(boundRoot);
                    }
                    if (category.equals("C")) {
                        result.fieldRoots.computeIfAbsent(category, ignored -> new TreeSet<>()).add(boundRoot);
                    }
                }
                effects.add(selected);
            }
            if (helper.onlyExceptional) context.terminatePaths(true);
            else context.mergeHelperAlternatives(effects);
        } else if (target instanceof ExecutableElement called
                && called.getKind() == ElementKind.CONSTRUCTOR
                && called.getEnclosingElement().toString().equals("java.lang.Object")) {
            // Object's zero-argument constructor has no user-defined effects.
        } else if (target instanceof ExecutableElement called && owner.equals(called.getEnclosingElement())) {
            limitations.add(result.id + ": delegation to " + called + " is not expanded");
        } else if (target instanceof ExecutableElement called && !owner.equals(called.getEnclosingElement())) {
            limitations.add(result.id + ": effects of external call " + called.getEnclosingElement() + "#" + called + " are unknown");
        } else if (!(target instanceof ExecutableElement)) {
            limitations.add(result.id + ": unresolved delegation is not credited");
        }
        return null;
    }
}
