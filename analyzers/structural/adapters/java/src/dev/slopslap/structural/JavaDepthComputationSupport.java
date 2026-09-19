package dev.slopslap.structural;

import com.sun.source.tree.Tree;
import com.sun.source.util.TreePath;

/** Shared path and bounded-work primitives for computation canonicalization. */
final class JavaDepthComputationSupport {
    private JavaDepthComputationSupport() { }
    static TreePath enclosing(TreePath path, Class<?> kind) {
        for (TreePath current = path; current != null; current = current.getParentPath()) {
            if (kind.isInstance(current.getLeaf())) return current;
        }
        return null;
    }
    static final class Limit extends RuntimeException { }
    static TreePath child(TreePath parent, Tree child) {
        if (parent == null || child == null) return null;
        TreePath path = TreePath.getPath(parent, child);
        return path == null ? new TreePath(parent, child) : path;
    }
}
