final class StateCoherentBefore {
  private int total;

  int advance(int delta) {
    if (delta < 0) return total;
    total += delta;
    if (total > 100) total = 100;
    return total;
  }

  int advanceAgain(int delta) {
    if (delta < 0) return total;
    total += delta;
    if (total > 100) total = 100;
    return total;
  }
}
