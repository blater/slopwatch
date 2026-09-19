package io.riverdb.engine.row;

import io.riverdb.base.error.StatusCode;
import io.riverdb.base.type.SqlValueBuffer;
import io.riverdb.engine.schema.TableDescriptor;
import java.nio.ByteBuffer;

/** Canonical, bounded codec for descriptor-shaped stored table rows. Instances are caller-owned. */
public final class StoredTableRowCodec {
  private final StoredTableRowDecoder decoder = new StoredTableRowDecoder();

  public StatusCode encode(
      TableDescriptor descriptor,
      long logicalRowId,
      SqlValueBuffer values,
      ByteBuffer target,
      int start,
      StoredTableRowEncodeResult result) {
    return StoredTableRowEncoder.encode(
        descriptor, logicalRowId, values, target, start, result);
  }

  public StatusCode decode(
      TableDescriptor descriptor,
      long expectedLogicalRowId,
      ByteBuffer source,
      int start,
      int length,
      SqlValueBuffer destination) {
    return decoder.decode(
        descriptor, expectedLogicalRowId, source, start, length, destination);
  }
}
