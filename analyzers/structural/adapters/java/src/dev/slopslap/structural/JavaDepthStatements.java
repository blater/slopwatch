package dev.slopslap.structural;

import com.sun.source.tree.BinaryTree;
import com.sun.source.tree.BlockTree;
import com.sun.source.tree.AssignmentTree;
import com.sun.source.tree.ConditionalExpressionTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.ExpressionStatementTree;
import com.sun.source.tree.IfTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.ThrowTree;
import com.sun.source.tree.VariableTree;
import com.sun.source.util.TreePath;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.List;
import java.util.Map;

/** Lowers statement and return forms shared by the scalar control-flow builder. */
final class JavaDepthStatements {
    private final JavaDepthFunction host;
    private final JavaDepthBranches branches;
    private final JavaDepthErrors errors;

    JavaDepthStatements(JavaDepthFunction host, JavaDepthBranches branches) {
        this.host = host; this.branches = branches; this.errors = new JavaDepthErrors(host);
    }

    void lower(List<? extends StatementTree> input) {
        for (StatementTree statement : input) {
            if (host.terminated()) return;
            if (statement instanceof IfTree branch) { branches.lower(branch); continue; }
            if (statement instanceof ReturnTree returned) {
                lowerReturnStatement(returned);
                continue;
            }
            if (statement instanceof ThrowTree throwing) {
                if (!errors.lower(throwing.getExpression())) host.unknownValue();
                continue;
            }
            if (statement instanceof VariableTree variable && lowerVariable(variable)) continue;
            host.unknownValue();
        }
    }

    void lowerCarrierConstructor(List<VariableElement> fields) {
        initializeFields(fields);
        MethodTree constructor = (MethodTree) host.methodPath().getLeaf();
        List<? extends StatementTree> input = constructor.getBody() == null
                ? List.of() : constructor.getBody().getStatements();
        int start = objectSuperIndex(input);
        if (start < 0) {
            host.unknownValue();
            return;
        }
        for (int index = start; index < input.size(); index++) {
            StatementTree statement = input.get(index);
            if (statement instanceof IfTree branch) {
                branches.lower(branch);
            } else if (statement instanceof ExpressionStatementTree expression
                    && expression.getExpression() instanceof AssignmentTree assignment
                    && lowerFieldAssignment(assignment, fields)) {
                continue;
            } else {
                host.unknownValue();
            }
            if (host.terminated()) return;
        }
    }

    private void initializeFields(List<VariableElement> fields) {
        for (VariableElement field : fields) {
            if (field.getModifiers().contains(javax.lang.model.element.Modifier.STATIC)) continue;
            Object constant = field.getConstantValue();
            if (constant != null) {
                String type = field.asType().toString();
                host.values().put(field, host.emitValue(Map.of("opcode", "constant", "type", type,
                        "value_kind", JavaDepthTypes.kind(field.asType()),
                        "value", Map.of("type", type, "value_kind", JavaDepthTypes.kind(field.asType()),
                                "constant", constant.toString()))));
            }
        }
    }

    private boolean lowerFieldAssignment(AssignmentTree assignment, List<VariableElement> fields) {
        Element element = host.trees().getElement(TreePath.getPath(host.methodPath(), assignment.getVariable()));
        if (!(element instanceof VariableElement field) || !fields.contains(field)
                || field.getModifiers().contains(javax.lang.model.element.Modifier.STATIC)) return false;
        ExpressionTree value = assignment.getExpression();
        if (!host.assignmentCompatibleWith(field.asType(), host.typeOf(value), value)) return false;
        host.values().put(field, host.expressionValue(value, field.asType()));
        return true;
    }

    private int objectSuperIndex(List<? extends StatementTree> statements) {
        if (statements.isEmpty() || !(statements.get(0) instanceof ExpressionStatementTree expression)
                || !(expression.getExpression() instanceof MethodInvocationTree call)) return 0;
        Element target = host.trees().getElement(TreePath.getPath(host.methodPath(), call));
        if (!(target instanceof ExecutableElement constructor)
                || constructor.getKind() != ElementKind.CONSTRUCTOR
                || !(constructor.getEnclosingElement() instanceof TypeElement type)
                || !type.getQualifiedName().contentEquals("java.lang.Object")
                || !call.getArguments().isEmpty()) return 0;
        return 1;
    }

    private void lowerReturnStatement(ReturnTree returned) {
        if (returned.getExpression() == null) {
            if (host.returnType() != null && host.returnType().getKind() == TypeKind.VOID) {
                host.emitValue(Map.of("opcode", "return", "operands", List.of()));
            } else {
                host.unknownValue();
            }
            return;
        }
        lowerReturn(returned.getExpression());
    }

    private boolean lowerVariable(VariableTree variable) {
        if (variable.getInitializer() == null) return false;
        TreePath path = TreePath.getPath(host.methodPath(), variable);
        TypeMirror declared = host.trees().getTypeMirror(path);
        ExpressionTree initializer = variable.getInitializer();
        if (!host.assignmentCompatibleWith(declared, host.typeOf(initializer), initializer)) return false;
        host.values().put(host.trees().getElement(path), host.expressionValue(initializer, declared));
        return true;
    }

    void lowerReturn(ExpressionTree expression) {
        TypeMirror result = host.returnType();
        if (isBooleanShortCircuit(expression, result)) {
            lowerBooleanReturn(expression);
            return;
        }
        if (expression instanceof ConditionalExpressionTree conditional) {
            lowerConditionalReturn(conditional);
            return;
        }
        String value = host.assignmentCompatibleWith(result, host.typeOf(expression), expression)
                ? host.expressionValue(expression, result) : host.unknownValue();
        host.emitValue(Map.of("opcode", "return", "operands", List.of(value)));
    }

    private boolean isBooleanShortCircuit(ExpressionTree expression, TypeMirror result) {
        if (!(expression instanceof BinaryTree binary) || result == null
                || result.getKind() != TypeKind.BOOLEAN) return false;
        Tree.Kind kind = binary.getKind();
        return kind == Tree.Kind.CONDITIONAL_AND || kind == Tree.Kind.CONDITIONAL_OR;
    }

    private void lowerBooleanReturn(ExpressionTree condition) {
        JavaDepthFunction.Block yes = host.newBlock(), no = host.newBlock();
        branches.lowerCondition(condition, yes, no);
        host.currentBlock(yes);
        emitBooleanReturn(true);
        host.currentBlock(no);
        emitBooleanReturn(false);
        host.currentBlock(host.newBlock());
    }

    private void emitBooleanReturn(boolean value) {
        String constant = host.emitValue(Map.of("opcode", "constant", "type", "boolean",
                "value_kind", "boolean", "value", Map.of("type", "boolean",
                        "value_kind", "boolean", "constant", Boolean.toString(value))));
        host.emitValue(Map.of("opcode", "return", "operands", List.of(constant)));
    }

    private void lowerConditionalReturn(ConditionalExpressionTree tree) {
        JavaDepthFunction.Block yes = host.newBlock(), no = host.newBlock();
        branches.lowerCondition(tree.getCondition(), yes, no);
        host.currentBlock(yes);
        emitReturnValue(tree.getTrueExpression());
        host.currentBlock(no);
        emitReturnValue(tree.getFalseExpression());
        host.currentBlock(host.newBlock());
    }

    private void emitReturnValue(ExpressionTree expression) {
        TypeMirror result = host.returnType();
        String value = host.assignmentCompatibleWith(result, host.typeOf(expression), expression)
                ? host.expressionValue(expression, result) : host.unknownValue();
        host.emitValue(Map.of("opcode", "return", "operands", List.of(value)));
    }
}
