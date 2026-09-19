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

final class JavaDepthMinimumScannerExceptionFlow {
    private final JavaDepthMinimumScannerCore context;
    private final JavaDepthMinimumScannerTraversal traversal;
    private final JavaDepthMinimumScannerServices services;
    private final Set<String> limitations;
    private final JavaDepthMinimumBehavior.Summary result;
    private final JavaDepthMinimumScannerExceptionTypes types;

    JavaDepthMinimumScannerExceptionFlow(JavaDepthMinimumScannerCore context,
            JavaDepthMinimumScannerTraversal traversal,
            JavaDepthMinimumScannerServices services,
            JavaDepthMinimumScannerExceptionTypes types) {
        this.context = context;
        this.traversal = traversal;
        this.services = services;
        this.limitations = context.limitations;
        this.result = context.result;
        this.types = types;
    }

    private Void scan(Tree tree, Void unused) { return traversal.scan(tree, unused); }
    private TreePath getCurrentPath() { return traversal.currentPath(); }

    public Void visitThrow(ThrowTree tree, Void unused) {
        context.tick();
        TypeElement thrown = types.thrownType(tree.getExpression(), getCurrentPath());
        for (JavaDepthMinimumBehavior.PathAlternative path : context.paths) if (path.live) path.pendingExceptionType = thrown;
        if (!services.recognizedError(tree.getExpression(), getCurrentPath())) {
            context.host.exhausted = true;
            limitations.add(result.id + ": unsupported thrown value keeps bounded validation unknown");
        }
        if (thrown == null) limitations.add(result.id + ": unresolved thrown type keeps exceptional flow unknown");
        Void value = traversal.scan(tree.getExpression(), unused);
        context.terminatePaths(true);
        return value;
    }

    public Void visitTry(TryTree tree, Void unused) {
        context.tick();
        if (!tree.getResources().isEmpty()) {
            context.unsupportedControl(tree);
            return null;
        }
        List<JavaDepthMinimumBehavior.PathAlternative> alreadyCompleted = new ArrayList<>();
        List<JavaDepthMinimumBehavior.PathAlternative> entry = new ArrayList<>();
        for (JavaDepthMinimumBehavior.PathAlternative path : context.paths) {
            if (path.live) entry.add(path.copy()); else alreadyCompleted.add(path.copy());
        }
        if (entry.isEmpty()) return null;
        context.paths = entry;
        traversal.scan(tree.getBlock(), unused);

        List<JavaDepthMinimumBehavior.PathAlternative> normal = new ArrayList<>();
        List<JavaDepthMinimumBehavior.PathAlternative> pending = new ArrayList<>();
        for (JavaDepthMinimumBehavior.PathAlternative path : context.paths) {
            if (path.exceptional) pending.add(path);
            else normal.add(path);
        }
        List<JavaDepthMinimumBehavior.PathAlternative> caught = new ArrayList<>();
        for (CatchTree catcher : tree.getCatches()) {
            List<JavaDepthMinimumBehavior.PathAlternative> matching = new ArrayList<>();
            List<JavaDepthMinimumBehavior.PathAlternative> remaining = new ArrayList<>();
            for (JavaDepthMinimumBehavior.PathAlternative path : pending) {
                if (types.matchesCatch(path, catcher)) matching.add(path);
                else remaining.add(path);
            }
            pending = remaining;
            if (matching.isEmpty()) continue;
            for (JavaDepthMinimumBehavior.PathAlternative path : matching) {
                path.live = true;
                path.exceptional = false;
                path.pendingExceptionType = null;
            }
            context.paths = matching;
            traversal.scan(catcher.getBlock(), unused);
            caught.addAll(context.paths);
        }

        List<JavaDepthMinimumBehavior.PathAlternative> incoming = new ArrayList<>(normal.size() + caught.size() + pending.size());
        incoming.addAll(normal);
        incoming.addAll(caught);
        incoming.addAll(pending);
        if (tree.getFinallyBlock() == null) {
            incoming.addAll(alreadyCompleted);
            context.paths = context.deduplicatePaths(incoming);
            return null;
        }

        // Preserve completion in a local frame, so nested finally blocks
        // cannot overwrite an outer pending return, throw or break.
        List<JavaDepthMinimumBehavior.PathAlternative> completed = new ArrayList<>(alreadyCompleted);
        for (JavaDepthMinimumBehavior.PathAlternative pendingCompletion : incoming) {
            JavaDepthMinimumBehavior.PathAlternative activePath = pendingCompletion.copy();
            activePath.live = true;
            activePath.exceptional = false;
            activePath.pendingExceptionType = null;
            activePath.jumpTarget = null;
            context.paths = new ArrayList<>(List.of(activePath));
            traversal.scan(tree.getFinallyBlock(), unused);
            for (JavaDepthMinimumBehavior.PathAlternative path : context.paths) {
                if (path.live) {
                    path.live = pendingCompletion.live;
                    path.exceptional = pendingCompletion.exceptional;
                    path.pendingExceptionType = pendingCompletion.pendingExceptionType;
                    path.jumpTarget = pendingCompletion.jumpTarget;
                }
                completed.add(path);
            }
        }
        context.paths = context.deduplicatePaths(completed);
        return null;
    }
}
