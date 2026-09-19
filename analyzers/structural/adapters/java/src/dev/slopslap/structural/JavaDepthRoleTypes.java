package dev.slopslap.structural;

import javax.lang.model.element.ExecutableElement;
import javax.lang.model.type.ArrayType;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;

final class JavaDepthRoleTypes {
    private JavaDepthRoleTypes() {}

    static boolean hasErrorType(ExecutableElement method) {
        if (hasErrorType(method.getReturnType())) return true;
        return method.getParameters().stream().anyMatch(parameter -> hasErrorType(parameter.asType()));
    }

    static boolean hasErrorType(TypeMirror type) {
        if (type == null) return false;
        if (type.getKind() == TypeKind.ERROR) return true;
        if (type.getKind() == TypeKind.ARRAY) return hasErrorType(((ArrayType) type).getComponentType());
        if (!(type instanceof DeclaredType declared)) return false;
        return declared.getTypeArguments().stream().anyMatch(JavaDepthRoleTypes::hasErrorType);
    }
}
