package dev.slopslap.structural;

import com.sun.source.tree.BlockTree;
import com.sun.source.tree.VariableTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.VariableElement;
import java.util.Map;
import java.util.Set;

/** Package façade for the separate carrier member proof stages. */
final class JavaDepthPassiveResultCarrierMembers {
    private JavaDepthPassiveResultCarrierMembers() { }
    static final class Counts { int constructors; int mutators; }

    static boolean pureInitializer(Trees trees, TreePath ownerPath, VariableTree variable,
                                   Map<Element, VariableElement> fields) {
        return JavaDepthPassiveResultCarrierValues.pureInitializer(trees, ownerPath, variable, fields);
    }

    static boolean getter(Trees trees, TreePath methodPath, ExecutableElement method,
                          Map<Element, VariableElement> fields, Set<VariableElement> getters) {
        return JavaDepthPassiveResultCarrierValues.getter(trees, methodPath, method, fields, getters);
    }

    static boolean directAssignments(Trees trees, TreePath methodPath, BlockTree body,
                                     ExecutableElement method, Map<Element, VariableElement> fields) {
        return JavaDepthPassiveResultCarrierAssignments.directAssignments(trees, methodPath, body, method, fields);
    }
}
