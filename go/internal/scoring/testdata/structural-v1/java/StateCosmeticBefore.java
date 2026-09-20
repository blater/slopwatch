final class StateCosmeticBefore {
  private int total;
  private int updates;

  int advance(int delta) {
    if (delta < 0) return total;
    total += delta;
    if (total > 100) total = 100;
    updates++;
    return total;
  }
}
