package dev.slopslap.structural;

import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.VariableElement;
import java.util.List;
import java.util.Map;

/** Package façade for passive-enum member proofs. */
final class JavaDepthPassiveEnumMembers {
    private JavaDepthPassiveEnumMembers() { }

    static boolean staticField(Trees trees, TreePath ownerPath, VariableElement field) {
        return JavaDepthPassiveEnumFields.staticField(trees, field);
    }
    static boolean metadataField(VariableElement field) { return JavaDepthPassiveEnumFields.metadataField(field); }
    static boolean constructor(Trees trees, ExecutableElement constructor, List<VariableElement> fields,
                               Map<String, VariableElement> byName) {
        return JavaDepthPassiveEnumConstructors.constructor(trees, constructor, fields, byName);
    }
    static boolean isAccessor(Trees trees, ExecutableElement method, Map<String, VariableElement> fields) {
        return JavaDepthPassiveEnumAccessors.instance(trees, method, fields);
    }
    static boolean isStaticAccessor(Trees trees, ExecutableElement method, Map<String, VariableElement> fields) {
        return JavaDepthPassiveEnumAccessors.staticAccessor(trees, method, fields);
    }
}
