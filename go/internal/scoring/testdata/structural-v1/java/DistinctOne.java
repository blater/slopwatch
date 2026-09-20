final class DistinctOne {
  static int run(int value) {
    if (value > 0) {
      if (value % 2 == 0) return value * 2;
    }
    return value;
  }
}
