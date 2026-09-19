package dev.slopslap.structural;

import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.TypeElement;

/** Bounded statelessness proof shared by instance boundary admission and calls. */
final class JavaDepthInstancePurity {
    private JavaDepthInstancePurity() { }

    static JavaDepthInstancePurityGraph.Graph graph(Trees trees, TreePath ownerPath, TypeElement owner) {
        return new JavaDepthInstancePurityGraph.Graph(trees, ownerPath, owner);
    }
}
