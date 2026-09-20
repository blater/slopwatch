final class UnusedBefore {
  static int run(int x) {
    int used = x * 2;
    if (x > 0) {
      int unused = x * 3;
    }
    return used + 1;
  }
}
