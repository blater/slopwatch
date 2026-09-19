package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.LiteralTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.NewClassTree;
import com.sun.source.tree.Tree;
import com.sun.source.util.TreePath;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.ModuleElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.TypeMirror;
import java.util.List;
import java.util.Map;

/** Lowers the exact standard argument-error constructors in the builtin registry. */
final class JavaDepthErrors {
    private static final String EMPTY_CONTRACT = "java.argument_error.empty";
    private static final String MESSAGE_CONTRACT = "java.argument_error.message";
    private static final String EXCEPTION = "java.lang.IllegalArgumentException";
    private static final String STRING = "java.lang.String";

    private final JavaDepthFunction host;

    JavaDepthErrors(JavaDepthFunction host) { this.host = host; }

    boolean lower(ExpressionTree expression) {
        if (!(expression instanceof NewClassTree created)) return false;
        ExecutableElement constructor = constructor(created);
        if (!standardConstructor(constructor, created)) {
            lowerArguments(created.getArguments());
            return false;
        }
        List<? extends ExpressionTree> arguments = created.getArguments();
        if (arguments.isEmpty()) {
            return emitError(EMPTY_CONTRACT, null);
        }
        if (arguments.size() == 1) {
            String message = constantMessage(arguments.get(0));
            if (message != null) return emitError(MESSAGE_CONTRACT, message);
        }
        lowerArguments(arguments);
        return false;
    }

    private ExecutableElement constructor(NewClassTree created) {
        Element element = host.trees().getElement(path(created));
        return element instanceof ExecutableElement constructor ? constructor : null;
    }

    private boolean standardConstructor(ExecutableElement constructor, NewClassTree created) {
        if (constructor == null || created.getClassBody() != null
                || constructor.getKind() != ElementKind.CONSTRUCTOR
                || !(constructor.getEnclosingElement() instanceof TypeElement type)
                || !type.getQualifiedName().contentEquals(EXCEPTION)
                || !inJavaBase(type)) return false;
        List<? extends VariableElement> parameters = constructor.getParameters();
        if (parameters.isEmpty()) return created.getArguments().isEmpty();
        return parameters.size() == 1 && parameters.get(0).asType().toString().equals(STRING)
                && created.getArguments().size() == 1;
    }

    private boolean inJavaBase(TypeElement type) {
        Element enclosing = type;
        while (enclosing != null && enclosing.getKind() != ElementKind.MODULE) {
            enclosing = enclosing.getEnclosingElement();
        }
        return enclosing instanceof ModuleElement module && module.getQualifiedName().contentEquals("java.base");
    }

    private String constantMessage(ExpressionTree expression) {
        TypeMirror type = host.typeOf(expression);
        if (type == null || !type.toString().equals(STRING)) return null;
        if (expression instanceof LiteralTree literal && literal.getValue() instanceof String value) return emitString(value);
        Element element = host.trees().getElement(path(expression));
        if (!(element instanceof VariableElement variable) || !(variable.getConstantValue() instanceof String value)
                || !safeConstantReference(expression)) return null;
        return emitString(value);
    }

    private String emitString(String value) {
        return host.emitValue(Map.of("opcode", "constant", "type", STRING, "value_kind", "string",
                "value", Map.of("type", STRING, "value_kind", "string", "constant", value)));
    }

    private boolean safeConstantReference(ExpressionTree expression) {
        if (expression instanceof IdentifierTree) return true;
        if (!(expression instanceof MemberSelectTree member)) return false;
        return host.trees().getElement(path(member.getExpression())) instanceof TypeElement;
    }

    private void lowerArguments(List<? extends ExpressionTree> arguments) {
        for (ExpressionTree argument : arguments) host.expressionValue(argument, host.typeOf(argument));
    }

    private boolean emitError(String contract, String message) {
        String value = host.emitValue(errorAllocation(contract, message));
        host.emitValue(Map.of("opcode", "throw", "operands", List.of(value)));
        return true;
    }

    private Map<String, Object> errorAllocation(String contract, String message) {
        String root = methodID() + "/" + host.currentBlock().id + "/error"
                + host.currentBlock().instructions.size();
        Map<String, Object> allocation = new java.util.LinkedHashMap<>();
        allocation.put("opcode", "allocate");
        allocation.put("type", "error");
        allocation.put("value_kind", "error_result");
        allocation.put("roots", List.of(Map.of("id", root, "kind", "allocation", "ownership", "owned")));
        allocation.put("provenance", List.of(Map.of("rule_id", contract, "fact_ids", List.of(contract))));
        if (message != null) allocation.put("field_bindings", List.of(Map.of("field", "error.message", "value", message)));
        return allocation;
    }

    private String methodID() {
        Element element = host.trees().getElement(host.methodPath());
        return element instanceof ExecutableElement method ? JavaDepthCalls.targetID(method) : "java.error";
    }

    private TreePath path(Tree tree) { return TreePath.getPath(host.methodPath(), tree); }
}
