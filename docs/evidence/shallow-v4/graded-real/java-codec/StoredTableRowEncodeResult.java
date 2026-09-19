package io.riverdb.engine.row;

/** Caller-owned publication result for one encoded stored table row. */
public final class StoredTableRowEncodeResult {
  private int length;

  public void reset() {
    length = 0;
  }

  public int length() {
    return length;
  }

  void setLength(int encodedLength) {
    length = encodedLength;
  }
}
