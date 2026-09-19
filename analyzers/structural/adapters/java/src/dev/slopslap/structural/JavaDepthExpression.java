package dev.slopslap.structural;

import com.sun.source.tree.BinaryTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.LiteralTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.ParenthesizedTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.UnaryTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/** Lowers the small, resolved scalar expression subset used by JavaDepthFunction. */
final class JavaDepthExpression {
    private final JavaDepthFunction host;

    JavaDepthExpression(JavaDepthFunction host) { this.host = host; }

    String lower(ExpressionTree tree, TypeMirror expected) {
        if (tree instanceof ParenthesizedTree nested) return lower(nested.getExpression(), expected);
        TypeMirror actual = host.typeOf(tree);
        if (tree instanceof MethodInvocationTree call) return lowerCall(call);
        String constant = lowerConstant(tree, actual, expected);
        if (constant != null) return constant;
        if (!host.assignmentCompatibleWith(expected, actual, tree)) return host.unknownValue();
        if (tree instanceof IdentifierTree identifier) return lowerIdentifier(identifier);
        if (tree instanceof MemberSelectTree member) return lowerField(member, expected);
        if (tree instanceof LiteralTree literal) return lowerLiteral(literal, expected);
        if (tree instanceof UnaryTree unary) return lowerUnary(unary, expected);
        if (tree instanceof BinaryTree binary) return lowerBinary(binary, expected);
        return host.unknownValue();
    }

    private String lowerConstant(ExpressionTree tree, TypeMirror actual, TypeMirror expected) {
        Element element = host.trees().getElement(path(tree));
        if (!(element instanceof VariableElement variable) || variable.getConstantValue() == null
                || !safeConstantReference(tree)
                || !JavaDepthTypes.scalar(actual) || !JavaDepthTypes.scalar(expected)
                || !compatibleConstant(expected, actual, tree)) return null;
        return constant(expected, variable.getConstantValue().toString());
    }

    private boolean compatibleConstant(TypeMirror expected, TypeMirror actual, Tree tree) {
        return host.assignmentCompatibleWith(expected, actual, tree)
                || expected.getKind() == TypeKind.LONG && actual.getKind() == TypeKind.INT;
    }

    private String lowerIdentifier(IdentifierTree tree) {
        Element element = host.trees().getElement(path(tree));
        if (element instanceof VariableElement field) {
            String fieldValue = lowerField(field, tree);
            if (fieldValue != null) return fieldValue;
        }
        String value = host.values().get(element);
        return value == null ? host.unknownValue() : value;
    }

    private String lowerField(MemberSelectTree tree, TypeMirror expected) {
        Element selected = host.trees().getElement(path(tree));
        if (!(selected instanceof VariableElement field)) return host.unknownValue();
        if (!(tree.getExpression() instanceof IdentifierTree receiver)
                || !receiver.getName().contentEquals("this")) return host.unknownValue();
        String value = lowerField(field, tree);
        return value == null ? host.unknownValue() : value;
    }

    private String lowerField(VariableElement field, Tree use) {
        if (!isReadableReceiverField(field, use)) return null;
        TypeMirror type = field.asType();
        return host.emitValue(Map.of("opcode", "field_read", "type", type.toString(),
                "value_kind", JavaDepthTypes.kind(type), "operands", List.of(host.receiverID()),
                "field_id", JavaDepthRoles.fieldID(field)));
    }

    private boolean isReadableReceiverField(VariableElement field, Tree use) {
        return !host.receiverID().isEmpty()
                && field.getEnclosingElement().equals(host.owner())
                && field.getModifiers().contains(Modifier.PRIVATE)
                && !field.getModifiers().contains(Modifier.STATIC)
                && !field.getModifiers().contains(Modifier.VOLATILE)
                && JavaDepthTypes.scalar(field.asType())
                && !(use instanceof MemberSelectTree member
                    && !(member.getExpression() instanceof IdentifierTree receiver
                        && receiver.getName().contentEquals("this")));
    }

    private String lowerLiteral(LiteralTree tree, TypeMirror expected) {
        return constant(expected, tree.getValue().toString());
    }

    private String constant(TypeMirror type, String value) {
        String literalType = type.toString();
        return host.emitValue(Map.of("opcode", "constant", "type", literalType,
                "value_kind", JavaDepthTypes.kind(type), "value", Map.of("type", literalType,
                        "value_kind", JavaDepthTypes.kind(type), "constant", value)));
    }

    private String lowerUnary(UnaryTree tree, TypeMirror result) {
        String operator = unaryOperator(tree.getKind());
        TypeMirror operand = host.typeOf(tree.getExpression());
        if (operator.isEmpty() || !JavaDepthTypes.scalar(result)
                || !host.assignmentCompatibleWith(result, operand, tree.getExpression())
                || operator.equals("!") && result.getKind() != TypeKind.BOOLEAN) return host.unknownValue();
        if (operator.equals("+")) return lower(tree.getExpression(), result);
        return primitive(result, JavaDepthTypes.mode(result), operator,
                List.of(lower(tree.getExpression(), result)));
    }

    private String unaryOperator(Tree.Kind kind) {
        return switch (kind) {
            case UNARY_PLUS -> "+";
            case UNARY_MINUS -> "-";
            case LOGICAL_COMPLEMENT -> "!";
            case BITWISE_COMPLEMENT -> "~";
            default -> "";
        };
    }

    private String lowerBinary(BinaryTree tree, TypeMirror result) {
        String operator = binaryOperator(tree.getKind());
        TypeMirror left = host.typeOf(tree.getLeftOperand());
        TypeMirror right = host.typeOf(tree.getRightOperand());
        if (operator.isEmpty() || !JavaDepthTypes.scalar(left) || !JavaDepthTypes.scalar(right)) {
            return host.unknownValue();
        }
        if (requiresNonzeroDivisor(operator) && !knownNonzero(tree.getRightOperand())) {
            return host.unknownValue();
        }
        TypeMirror operandType = promotedType(left, right);
        if (!validBinaryTypes(result, left, right, operandType)) return host.unknownValue();
        return primitive(result, JavaDepthTypes.mode(operandType), operator, List.of(
                lower(tree.getLeftOperand(), operandType), lower(tree.getRightOperand(), operandType)));
    }

    private String binaryOperator(Tree.Kind kind) {
        return switch (kind) {
            case PLUS -> "+";
            case MINUS -> "-";
            case MULTIPLY -> "*";
            case DIVIDE -> "/";
            case REMAINDER -> "%";
            case AND -> "&";
            case OR -> "|";
            case XOR -> "^";
            case LESS_THAN -> "<";
            case LESS_THAN_EQUAL -> "<=";
            case GREATER_THAN -> ">";
            case GREATER_THAN_EQUAL -> ">=";
            case EQUAL_TO -> "==";
            case NOT_EQUAL_TO -> "!=";
            default -> "";
        };
    }

    private boolean requiresNonzeroDivisor(String operator) {
        return operator.equals("/") || operator.equals("%");
    }

    private boolean knownNonzero(ExpressionTree tree) {
        if (tree instanceof ParenthesizedTree nested) return knownNonzero(nested.getExpression());
        if (tree instanceof UnaryTree unary && (unary.getKind() == Tree.Kind.UNARY_PLUS
                || unary.getKind() == Tree.Kind.UNARY_MINUS)) return knownNonzero(unary.getExpression());
        if (!safeConstantReference(tree)) return false;
        Element element = host.trees().getElement(path(tree));
        if (!(element instanceof VariableElement variable)) {
            return tree instanceof LiteralTree literal && nonzeroLiteral(literal.getValue());
        }
        Object value = variable.getConstantValue();
        return value != null && nonzeroLiteral(value);
    }

    private boolean safeConstantReference(ExpressionTree tree) {
        if (tree instanceof IdentifierTree || tree instanceof LiteralTree) return true;
        if (!(tree instanceof MemberSelectTree member)) return false;
        return host.trees().getElement(path(member.getExpression())) instanceof TypeElement;
    }

    private boolean nonzeroLiteral(Object value) {
        return value instanceof Number number && number.doubleValue() != 0.0d;
    }

    private TypeMirror promotedType(TypeMirror left, TypeMirror right) {
        return left.getKind() == TypeKind.LONG || right.getKind() == TypeKind.LONG
                ? left.getKind() == TypeKind.LONG ? left : right : left;
    }

    private boolean validBinaryTypes(TypeMirror result, TypeMirror left, TypeMirror right,
                                     TypeMirror operandType) {
        if (result.getKind() != TypeKind.BOOLEAN && result.getKind() != operandType.getKind()) return false;
        return result.getKind() != TypeKind.BOOLEAN || sameNumericKinds(left, right);
    }

    private boolean sameNumericKinds(TypeMirror left, TypeMirror right) {
        return left.getKind() == right.getKind()
                || left.getKind() == TypeKind.INT && right.getKind() == TypeKind.LONG
                || left.getKind() == TypeKind.LONG && right.getKind() == TypeKind.INT;
    }

    private String primitive(TypeMirror type, String mode, String operator, List<String> operands) {
        return host.emitValue(Map.of("opcode", "primitive", "type", type.toString(),
                "value_kind", JavaDepthTypes.kind(type), "operator", operator,
                "arithmetic_mode", mode, "operands", operands));
    }

    private String lowerCall(MethodInvocationTree call) {
        ExecutableElement method = JavaDepthCalls.resolve(host.trees(), host.methodPath(), call);
        boolean selection = JavaDepthCalls.supportedSelection(host.trees(), host.methodPath(), call, method);
        if (!selection) lowerReceiver(call);
        List<String> actuals = new ArrayList<>(call.getArguments().size());
        for (ExpressionTree argument : call.getArguments()) actuals.add(lower(argument, host.typeOf(argument)));
        if (!validCall(call, method, selection)) return host.unknownValue();
        List<Map<String, Object>> bindings = new ArrayList<>(actuals.size());
        for (int index = 0; index < actuals.size(); index++) {
            bindings.add(Map.of("formal", "arg" + index, "actual", actuals.get(index)));
        }
        Map<String, Object> target = Map.of("targets", List.of(JavaDepthCalls.targetID(method)), "bindings", bindings);
        TypeMirror result = host.typeOf(call);
        return host.emitValue(Map.of("opcode", "call", "type", result.toString(),
                "value_kind", JavaDepthTypes.kind(result), "operands", actuals, "call", target));
    }

    private boolean validCall(MethodInvocationTree call, ExecutableElement method, boolean selection) {
        if (method == null || !selection) return false;
        boolean instance = !method.getModifiers().contains(javax.lang.model.element.Modifier.STATIC);
        // Instance dispatch remains same-owner and therefore closed over the
        // boundary's receiver proof. Static calls may cross a source-backed
        // class boundary, but classpath/JDK bodies are never admitted.
        if (instance ? !JavaDepthCalls.sameOwner(method, host.owner())
                : !JavaDepthCalls.sameOwner(method, host.owner())
                        && (!JavaDepthCalls.samePackage(method, host.owner())
                                || !host.sourceMethods().contains(method))) return false;
        boolean validSelection = instance ? JavaDepthCalls.instanceSelection(call)
                : JavaDepthCalls.staticSelection(host.trees(), host.methodPath(), call, method);
        return validSelection && (instance ? JavaDepthCalls.dispatchable(method, host.owner()) : true)
                && JavaDepthCalls.supportedScalar(method)
                && JavaDepthCalls.exactArgumentTypes(host.trees(), host.methodPath(), call, method)
                && JavaDepthCalls.scalar(host.typeOf(call));
    }

    private void lowerReceiver(MethodInvocationTree call) {
        ExpressionTree select = call.getMethodSelect();
        if (select instanceof MemberSelectTree member) lower(member.getExpression(), host.typeOf(member.getExpression()));
    }

    private TreePath path(Tree tree) { return TreePath.getPath(host.methodPath(), tree); }
}
