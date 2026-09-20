final class StateCoherentAfter {
  private int total;

  private int advanceState(int delta) {
    if (delta < 0) return total;
    total += delta;
    if (total > 100) total = 100;
    return total;
  }

  int advance(int delta) { return advanceState(delta); }
  int advanceAgain(int delta) { return advanceState(delta); }
}
