package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/** Enum declaration and constant-shape checks for passive enum proofs. */
final class JavaDepthPassiveEnumInspector {
    private JavaDepthPassiveEnumInspector() { }

    static JavaDepthPassiveEnum.Proof inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        if (owner.getKind() != ElementKind.ENUM || !(ownerPath.getLeaf() instanceof ClassTree declaration)
                || !owner.getTypeParameters().isEmpty() || !owner.getInterfaces().isEmpty()
                || owner.getNestingKind().isNested() || declaration.getMembers().size() > 256) return null;
        List<VariableElement> constants = new ArrayList<>();
        List<VariableElement> fields = new ArrayList<>();
        Map<String, VariableElement> staticFields = new HashMap<>();
        for (Element element : owner.getEnclosedElements()) {
            if (element.getKind() == ElementKind.ENUM_CONSTANT && element instanceof VariableElement constant) constants.add(constant);
            if (element.getKind() != ElementKind.FIELD || !(element instanceof VariableElement field)
                    || trees.getPath(field) == null) continue;
            if (field.getModifiers().contains(Modifier.STATIC)) {
                if (!JavaDepthPassiveEnumMembers.staticField(trees, ownerPath, field)) return null;
                staticFields.put(field.getSimpleName().toString(), field);
            } else {
                if (!JavaDepthPassiveEnumMembers.metadataField(field)) return null;
                fields.add(field);
            }
        }
        if (constants.isEmpty() || !JavaDepthPassiveEnumConstants.arePlain(trees, ownerPath, declaration)) return null;
        Map<String, VariableElement> byName = new HashMap<>();
        for (VariableElement field : fields) byName.put(field.getSimpleName().toString(), field);
        List<ExecutableElement> constructors = new ArrayList<>();
        List<ExecutableElement> accessors = new ArrayList<>();
        List<ExecutableElement> staticAccessors = new ArrayList<>();
        for (Element element : owner.getEnclosedElements()) {
            if (!(element instanceof ExecutableElement executable)) continue;
            if (executable.getKind() == ElementKind.CONSTRUCTOR) {
                if (!JavaDepthPassiveEnumMembers.constructor(trees, executable, fields, byName)) return null;
                constructors.add(executable);
            } else if (JavaDepthPassiveEnumMembers.isAccessor(trees, executable, byName)) accessors.add(executable);
            else if (JavaDepthPassiveEnumMembers.isStaticAccessor(trees, executable, staticFields)) staticAccessors.add(executable);
            else if (trees.getPath(executable) != null) return null;
        }
        if (accessors.size() != fields.size()) return null;
        return new JavaDepthPassiveEnum.Proof(constructors, accessors, staticAccessors, constants, fields.size());
    }

}
