package blater.nql.domain;

import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.Map;

/** Caches non-repeated nodes created while mapping one result row. */
class HierarchyRowContext {
  private final IdentityHashMap<Node, Map<HierarchyPath, Node>> objects = new IdentityHashMap<>();

  Node objectChild(Node parent, HierarchyPath path) {
    return child(parent, path, path.getTerminalNodeName());
  }

  Node anonymousChild(Node parent, HierarchyPath path) {
    return child(parent, path, "");
  }

  private Node child(Node parent, HierarchyPath path, String name) {
    return objects.computeIfAbsent(parent, ignored -> new HashMap<>())
        .computeIfAbsent(path, ignored -> {
          Node child = new Node(name);
          parent.addNode(child);
          return child;
        });
  }
}
