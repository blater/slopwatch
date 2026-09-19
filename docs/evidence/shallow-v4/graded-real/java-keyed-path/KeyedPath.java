package blater.nql.domain;

import java.util.List;

/**
 * Declares the internal result columns that identify one object at an output path.
 */
public record KeyedPath(
    HierarchyPath identityPath,
    RepetitionPlacement placement,
    List<String> sourceColumns,
    KeyOrigin origin) {

  public KeyedPath(HierarchyPath path, List<String> sourceColumns) {
    this(path, RepetitionPlacement.named(), sourceColumns, KeyOrigin.EXPLICIT);
  }

  public KeyedPath {
    if (identityPath == null) throw new IllegalArgumentException("A keyed path requires an identity path.");
    if (placement == null) throw new IllegalArgumentException("A keyed path requires repetition placement.");
    sourceColumns = List.copyOf(sourceColumns);
    if (sourceColumns.isEmpty()) throw new IllegalArgumentException("A keyed path requires source columns.");
    origin = origin == null ? KeyOrigin.EXPLICIT : origin;
    if (origin == KeyOrigin.EXPLICIT && !(placement instanceof RepetitionPlacement.NamedItem)) {
      throw new IllegalArgumentException("Explicit keys must repeat their named identity path.");
    }
  }

  public HierarchyPath path() {
    return identityPath;
  }

  public boolean inferred() {
    return origin == KeyOrigin.INFERRED;
  }
}
