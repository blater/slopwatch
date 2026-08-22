package dev.slopslap.structural;

import com.sun.source.tree.*;
import java.util.*;

final class JavaTypeShapes {
    private JavaTypeShapes() {}

    static Facts.TypeShape shape(Tree tree) {
        Facts.TypeShape result = new Facts.TypeShape();
        result.kind = tree.getKind().toString().toLowerCase(Locale.ROOT);
        result.name = tree.toString();
        if (tree instanceof ParameterizedTypeTree parameterized) {
            for (Tree argument : parameterized.getTypeArguments()) result.children.add(shape(argument));
        } else if (tree instanceof ArrayTypeTree array) result.children.add(shape(array.getType()));
        result.complexity = 1;
        for (Facts.TypeShape child : result.children) result.complexity += child.complexity;
        result.complexity = Math.min(32, result.complexity);
        result.stableId = result.kind + ":" + result.name + ":" + result.complexity;
        return result;
    }
}
