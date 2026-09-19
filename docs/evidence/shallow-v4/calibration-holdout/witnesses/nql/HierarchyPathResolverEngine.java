package blater.nql.domain;

import blater.nql.runner.sql.domain.QueryResultRow;

import java.util.Optional;

/** Resolves mapped paths while preserving keyed and row-local node identity. */
final class HierarchyPathResolverEngine {
  private final HierarchyKeyIndex keyIndex = new HierarchyKeyIndex();

  HierarchyKeyIndex keyIndex() {
    return keyIndex;
  }

  Node resolveParent(
      Node root,
      Hierarchy.RootKind rootKind,
      MappingPlan plan,
      HierarchyPath path,
      QueryResultRow row,
      HierarchyRowContext rowContext) {
    if (path == null) {
      return root;
    }
    if (rootKind == Hierarchy.RootKind.NAMED && path.isRoot()) {
      return root;
    }

    Node current = root;
    HierarchyPath currentPath = initialPath(rootKind, path);
    int start = rootKind == Hierarchy.RootKind.NAMED ? 1 : 0;
    for (int index = start; index < path.getPathParts().size(); index++) {
      currentPath = nextPath(currentPath, path.getPathParts().get(index));
      current = resolveStep(current, root, rootKind, plan, currentPath, row, rowContext);
      if (current == null) {
        return null;
      }
    }
    return current;
  }

  private Node resolveStep(
      Node current,
      Node root,
      Hierarchy.RootKind rootKind,
      MappingPlan plan,
      HierarchyPath path,
      QueryResultRow row,
      HierarchyRowContext rowContext) {
    KeyedPath repeated = repeatedPath(plan, path);
    if (isSyntheticArrayOwner(rootKind, plan, path, repeated)) {
      return syntheticArrayItem(root, plan, path, row, rowContext);
    }
    if (repeated != null) {
      return repeatedChild(current, path, repeated, row, rowContext);
    }
    if (HierarchyPathPlan.isInferredOwner(plan, path)) {
      return keyIndex.singletonChild(current, path);
    }
    if (HierarchyPathPlan.isObjectPath(plan, path)) {
      return rowContext.objectChild(current, path);
    }
    return keyIndex.singletonChild(current, path);
  }

  private Node syntheticArrayItem(
      Node root,
      MappingPlan plan,
      HierarchyPath path,
      QueryResultRow row,
      HierarchyRowContext rowContext) {
    KeyedPath owner = keyedPath(plan, path);
    HierarchyKeyIndex.KeyState state = keyIndex.keyState(owner, row);
    if (state == HierarchyKeyIndex.KeyState.ABSENT) {
      return null;
    }
    Node item = state == HierarchyKeyIndex.KeyState.PARTIAL
        ? rowContext.anonymousChild(root, owner.path())
        : keyIndex.keyedAnonymousChild(root, owner.path(), keyIndex.keyTuple(owner, row));
    return keyIndex.singletonChild(item, path);
  }

  private boolean isSyntheticArrayOwner(
      Hierarchy.RootKind rootKind,
      MappingPlan plan,
      HierarchyPath path,
      KeyedPath repeated) {
    if (repeated != null || rootKind != Hierarchy.RootKind.SYNTHETIC_ARRAY) {
      return false;
    }
    KeyedPath owner = keyedPath(plan, path);
    return owner != null
        && owner.placement() instanceof RepetitionPlacement.AnonymousItem anonymous
        && anonymous.containerPath().isEmpty();
  }

  private Node repeatedChild(
      Node current,
      HierarchyPath path,
      KeyedPath repeated,
      QueryResultRow row,
      HierarchyRowContext rowContext) {
    HierarchyKeyIndex.KeyState state = keyIndex.keyState(repeated, row);
    if (state == HierarchyKeyIndex.KeyState.ABSENT) {
      return null;
    }
    if (state == HierarchyKeyIndex.KeyState.PARTIAL && !repeated.inferred()) {
      throw new IllegalStateException("Partially null structure key: " + path);
    }
    if (repeated.placement() instanceof RepetitionPlacement.AnonymousItem) {
      Node collection = keyIndex.singletonChild(current, path);
      collection.setCollection(true);
      return anonymousRepeatedChild(collection, repeated, state, row, rowContext);
    }
    return namedRepeatedChild(current, path, repeated, state, row, rowContext);
  }

  private Node anonymousRepeatedChild(
      Node collection,
      KeyedPath repeated,
      HierarchyKeyIndex.KeyState state,
      QueryResultRow row,
      HierarchyRowContext rowContext) {
    return state == HierarchyKeyIndex.KeyState.PARTIAL
        ? rowContext.anonymousChild(collection, repeated.path())
        : keyIndex.keyedAnonymousChild(
            collection, repeated.path(), keyIndex.keyTuple(repeated, row));
  }

  private Node namedRepeatedChild(
      Node current,
      HierarchyPath path,
      KeyedPath repeated,
      HierarchyKeyIndex.KeyState state,
      QueryResultRow row,
      HierarchyRowContext rowContext) {
    return state == HierarchyKeyIndex.KeyState.PARTIAL
        ? rowContext.objectChild(current, path)
        : keyIndex.keyedChild(current, path, keyIndex.keyTuple(repeated, row));
  }

  KeyedPath keyedPath(MappingPlan plan, HierarchyPath path) {
    return HierarchyPathPlan.keyedPath(plan, path);
  }

  KeyedPath repeatedPath(MappingPlan plan, HierarchyPath path) {
    return HierarchyPathPlan.repeatedPath(plan, path);
  }

  boolean flatRows(MappingPlan plan) {
    return HierarchyPathPlan.flatRows(plan);
  }

  void initializeInferredCollections(
      Node root, Hierarchy.RootKind rootKind, MappingPlan plan) {
    for (KeyedPath key : plan.getKeyedPaths()) {
      if (key.placement() instanceof RepetitionPlacement.AnonymousItem anonymous) {
        initializeCollection(root, rootKind, anonymous.containerPath());
      }
    }
  }

  private void initializeCollection(
      Node root, Hierarchy.RootKind rootKind, Optional<HierarchyPath> path) {
    if (path.isEmpty()) {
      root.setCollection(true);
      return;
    }
    collectionNode(root, rootKind, path.get()).setCollection(true);
  }

  private Node collectionNode(Node root, Hierarchy.RootKind rootKind, HierarchyPath path) {
    Node current = root;
    HierarchyPath currentPath = initialPath(rootKind, path);
    int start = rootKind == Hierarchy.RootKind.NAMED ? 1 : 0;
    for (int index = start; index < path.getPathParts().size(); index++) {
      currentPath = nextPath(currentPath, path.getPathParts().get(index));
      current = keyIndex.singletonChild(current, currentPath);
    }
    return current;
  }

  private static HierarchyPath initialPath(Hierarchy.RootKind rootKind, HierarchyPath path) {
    return rootKind == Hierarchy.RootKind.NAMED
        ? HierarchyPath.fromDottedPath(path.getRootName())
        : null;
  }

  private static HierarchyPath nextPath(HierarchyPath current, String name) {
    return current == null ? HierarchyPath.fromDottedPath(name) : current.child(name);
  }
}
