final class StateCosmeticAfter {
  private int total;
  private int updates;

  private boolean acceptState(int delta) {
    if (delta < 0) return false;
    return true;
  }
  private void setState(int delta) {
    total += delta;
    if (total > 100) total = 100;
  }
  private void markState() { updates++; }

  int advance(int delta) {
    if (!acceptState(delta)) return total;
    setState(delta);
    markState();
    return total;
  }
}
