package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Resolved scalar lowering. Unsupported syntax remains explicit. */
final class JavaDepthFunction {
    private final Trees trees;
    private final TreePath methodPath;
    private final Set<ExecutableElement> sourceMethods;
    private TypeMirror returnType;
    private TypeElement owner;
    private boolean instance;
    private final Map<Element, String> values = new HashMap<>();
    private final List<Block> blocks = new ArrayList<>();
    private final JavaDepthExpression expressions;
    private final JavaDepthBranches branches;
    private final JavaDepthStatements statements;
    private Block current;
    private int nextBlock;
    private int nextInstruction;

    JavaDepthFunction(Trees trees, TreePath path) {
        this(trees, path, Set.of());
    }

    JavaDepthFunction(Trees trees, TreePath path, Set<ExecutableElement> sourceMethods) {
        this.trees = trees;
        this.methodPath = path;
        this.sourceMethods = sourceMethods == null ? Set.of() : sourceMethods;
        this.expressions = new JavaDepthExpression(this);
        this.branches = new JavaDepthBranches(this);
        this.statements = new JavaDepthStatements(this, branches);
        branches.statements(statements);
    }

    Map<String, Object> lower(String id, ExecutableElement method) {
        List<Object> formals = begin(id, method);
        MethodTree tree = (MethodTree) methodPath.getLeaf();
        if (tree.getBody() == null) unknown();
        else statements.lower(tree.getBody().getStatements());
        if (returnType.getKind() == TypeKind.VOID && !terminated()) {
            emit(Map.of("opcode", "return", "operands", List.of()));
        }
        List<Object> results = returnType.getKind() == TypeKind.VOID ? List.of()
                : List.of(JavaDepthTypes.formal("result0", "", method.getReturnType()));
        return finish(id, formals, results);
    }

    List<Object> begin(String id, ExecutableElement method) {
        returnType = method.getReturnType();
        owner = (TypeElement) method.getEnclosingElement();
        instance = !method.getModifiers().contains(javax.lang.model.element.Modifier.STATIC);
        current = new Block("entry");
        blocks.add(current);
        List<Object> formals = new ArrayList<>();
        for (int index = 0; index < method.getParameters().size(); index++) {
            var parameter = method.getParameters().get(index);
            String name = "arg" + index;
            values.put(parameter, name);
            formals.add(JavaDepthTypes.formal(name, id + "/" + name, parameter.asType()));
        }
        return formals;
    }

    Map<String, Object> finish(String id, List<Object> formals, List<Object> results) {
        List<Object> encodedBlocks = new ArrayList<>();
        for (Block block : blocks) {
            encodedBlocks.add(Map.of("id", block.id, "instructions", block.instructions, "edges", block.edges));
        }
        Map<String, Object> function = new LinkedHashMap<>();
        function.put("id", id);
        function.put("entry", "entry");
        function.put("formals", formals);
        function.put("results", results);
        function.put("blocks", encodedBlocks);
        if (instance && hasReceiverReads()) {
            function.put("receiver", owner.getQualifiedName().toString());
            function.put("receiver_formal", Map.of("id", receiverID(),
                    "path", id + "/receiver", "type", owner.getQualifiedName().toString(),
                    "concept", "reference", "value_kind", "reference"));
        }
        return function;
    }

    private boolean hasReceiverReads() {
        return blocks.stream().flatMap(block -> block.instructions.stream())
                .anyMatch(instruction -> "field_read".equals(instruction.get("opcode")));
    }

    boolean terminated() {
        if (current.instructions.isEmpty()) return false;
        String opcode = (String) current.instructions.get(current.instructions.size() - 1).get("opcode");
        return "return".equals(opcode) || "throw".equals(opcode);
    }

    Block newBlock() {
        Block block = new Block("b" + (++nextBlock));
        blocks.add(block);
        return block;
    }

    void edge(Block from, Block to, String kind, String guard) {
        from.edges.add(JavaDepthControl.edge(from.id, to.id, kind, guard));
    }

    private boolean assignmentCompatible(TypeMirror expected, TypeMirror actual, Tree tree) {
        if (expected == null || actual == null || !JavaDepthTypes.scalar(expected) || !JavaDepthTypes.scalar(actual)) return false;
        if (expected.getKind() == actual.getKind()) return true;
        return expected.getKind() == TypeKind.LONG && actual.getKind() == TypeKind.INT
                && tree instanceof LiteralTree;
    }

    Trees trees() { return trees; }

    Set<ExecutableElement> sourceMethods() { return sourceMethods; }
    TreePath methodPath() { return methodPath; }
    TypeElement owner() { return owner; }
    TypeMirror returnType() { return returnType; }
    String receiverID() { return instance ? "this" : ""; }
    Map<Element, String> values() { return values; }
    TypeMirror typeOf(Tree tree) {
        TreePath path = TreePath.getPath(methodPath, tree);
        return path == null ? null : trees.getTypeMirror(path);
    }
    boolean assignmentCompatibleWith(TypeMirror expected, TypeMirror actual, Tree tree) {
        return assignmentCompatible(expected, actual, tree);
    }
    String emitValue(Map<String, Object> fields) { return emit(fields); }
    String unknownValue() { return unknown(); }
    String expressionValue(ExpressionTree tree, TypeMirror expected) { return expressions.lower(tree, expected); }
    void lowerCarrierConstructorStatements(List<VariableElement> fields) { statements.lowerCarrierConstructor(fields); }
    Block currentBlock() { return current; }
    void currentBlock(Block block) { current = block; }

    private String unknown() { return emit(Map.of("opcode", "unknown", "type", "unknown", "value_kind", "unknown")); }

    private String emit(Map<String, Object> fields) {
        String id = "n" + (++nextInstruction);
        Map<String, Object> instruction = new LinkedHashMap<>(fields);
        instruction.put("id", id);
        if (!"return".equals(fields.get("opcode")) && !"throw".equals(fields.get("opcode"))) {
            instruction.put("results", List.of(id));
        }
        current.instructions.add(instruction);
        return id;
    }

    static final class Block {
        final String id;
        final List<Map<String, Object>> instructions = new ArrayList<>();
        final List<Map<String, Object>> edges = new ArrayList<>();
        Block(String id) { this.id = id; }
    }

    record BranchEnd(Block block, Map<Element, String> values, boolean live) { }
}
