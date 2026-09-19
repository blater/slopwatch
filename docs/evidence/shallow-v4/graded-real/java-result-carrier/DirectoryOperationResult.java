package io.riverdb.platform.file;

/**
 * Caller-owned result for synchronous directory operations.
 *
 * <p>The file is present only when the operation returns an open file capability. A non-OK status
 * may still report {@link DirectoryDurability#VISIBLE_NOT_DURABLE} or
 * {@link DirectoryDurability#UNKNOWN}; callers must not infer non-application from status alone.
 */
public final class DirectoryOperationResult {
  private DurableFile file;
  private DirectoryDurability durability = DirectoryDurability.NOT_APPLIED;

  public DurableFile file() {
    return file;
  }

  public DirectoryDurability durability() {
    return durability;
  }

  public void set(DurableFile file, DirectoryDurability durability) {
    this.file = file;
    this.durability = durability;
  }

  public void reset() {
    file = null;
    durability = DirectoryDurability.NOT_APPLIED;
  }
}
