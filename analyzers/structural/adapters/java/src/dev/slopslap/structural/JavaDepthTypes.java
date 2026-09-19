package dev.slopslap.structural;

import javax.lang.model.type.TypeMirror;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.element.TypeElement;
import java.util.Map;

final class JavaDepthTypes {
    static String kind(TypeMirror type) {
        if (type == null) return "unknown";
        return switch (type.getKind()) {
            case BYTE, SHORT, CHAR, INT, LONG -> "numeric";
            case BOOLEAN -> "boolean";
            default -> "unknown";
        };
    }
    static String concept(TypeMirror type) {
        if (type == null) return "unknown";
        return switch (type.getKind()) {
            case BYTE, SHORT, INT, LONG, CHAR, FLOAT, DOUBLE -> "number";
            case BOOLEAN -> "boolean";
            case DECLARED -> type instanceof DeclaredType declared
                    && declared.asElement() instanceof TypeElement element
                    && element.getQualifiedName().contentEquals("java.lang.String") ? "text" : "unknown";
            default -> "unknown";
        };
    }
    static Map<String, Object> formal(String id, String path, TypeMirror type) {
        return Map.of("id", id, "path", path, "type", type.toString(), "value_kind", kind(type));
    }
    static String mode(TypeMirror type) {
        return kind(type).equals("boolean") ? "boolean" : "wrapping";
    }
    static boolean scalar(TypeMirror type) {
        if (type == null) return false;
        TypeKind kind = type.getKind();
        return kind == TypeKind.BYTE || kind == TypeKind.SHORT || kind == TypeKind.CHAR
                || kind == TypeKind.INT || kind == TypeKind.LONG || kind == TypeKind.BOOLEAN;
    }
}
