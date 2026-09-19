package dev.slopslap.structural;

import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/** Lowers source-backed normalized flows for validated immutable carriers. */
final class JavaDepthCarrierLowering {
    private final JavaDepthFunction host;

    JavaDepthCarrierLowering(JavaDepthFunction host) {
        this.host = host;
    }

    JavaDepthCarrierFlow.ConstructorFlow constructor(String id, ExecutableElement method,
                                                      List<VariableElement> fields) {
        List<Object> formals = host.begin(id, method);
        host.lowerCarrierConstructorStatements(fields);
        List<String> values = new ArrayList<>();
        List<Object> results = new ArrayList<>();
        List<Map<String, Object>> initialFields = new ArrayList<>();
        int resultIndex = 0;
        for (VariableElement field : fields) {
            if (field.getModifiers().contains(Modifier.STATIC)) continue;
            String value = host.values().get(field);
            if (value == null) value = host.unknownValue();
            values.add(value);
            String fieldID = fieldID(field);
            results.add(JavaDepthTypes.formal("result" + resultIndex++, fieldID, field.asType()));
            initialFields.add(Map.of("field", fieldID, "value", id + "/" + value));
        }
        if (!host.terminated()) host.emitValue(Map.of("opcode", "return", "operands", values));
        return new JavaDepthCarrierFlow.ConstructorFlow(host.finish(id, formals, results), initialFields);
    }

    Map<String, Object> accessor(String id, ExecutableElement method, VariableElement field) {
        host.begin(id, method);
        List<Object> formals = new ArrayList<>();
        String value;
        if (field == null) {
            value = host.unknownValue();
        } else {
            value = fieldID(field);
            host.values().put(field, value);
            formals.add(JavaDepthTypes.formal(value, value, field.asType()));
        }
        if (!host.terminated()) host.emitValue(Map.of("opcode", "return", "operands", List.of(value)));
        return host.finish(id, formals, List.of(JavaDepthTypes.formal("result0", "", method.getReturnType())));
    }

    private String fieldID(VariableElement field) {
        return ((TypeElement) field.getEnclosingElement()).getQualifiedName() + "#" + field.getSimpleName();
    }
}
