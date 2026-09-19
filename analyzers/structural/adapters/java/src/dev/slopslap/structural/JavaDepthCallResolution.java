package dev.slopslap.structural;

import com.sun.source.tree.CompilationUnitTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import java.nio.file.Path;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Source-backed call lookup and stable target identifiers. */
final class JavaDepthCallResolution {
    private JavaDepthCallResolution() { }

    static ExecutableElement resolve(Trees trees, TreePath root, MethodInvocationTree call) {
        Element element = trees.getElement(TreePath.getPath(root, call));
        return element instanceof ExecutableElement method ? method : null;
    }

    static Map<ExecutableElement, TreePath> sourceMethodPaths(
            Trees trees, List<? extends CompilationUnitTree> units, Path workspace, Set<String> included) {
        Map<ExecutableElement, TreePath> result = new IdentityHashMap<>();
        for (CompilationUnitTree unit : units) {
            String file = workspace.relativize(Path.of(unit.getSourceFile().toUri())).toString().replace('\\', '/');
            if (included != null && !included.isEmpty() && !included.contains(file)) continue;
            new TreePathScanner<Void, Void>() {
                @Override public Void visitMethod(MethodTree tree, Void ignored) {
                    Element element = trees.getElement(getCurrentPath());
                    if (element instanceof ExecutableElement method) result.put(method, getCurrentPath());
                    return super.visitMethod(tree, ignored);
                }
            }.scan(unit, null);
        }
        return result;
    }

    static String targetID(ExecutableElement method) {
        TypeElement owner = (TypeElement) method.getEnclosingElement();
        return owner.getQualifiedName() + "#" + method;
    }
}
