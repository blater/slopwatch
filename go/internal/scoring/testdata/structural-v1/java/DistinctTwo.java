final class DistinctTwo {
  static int run(int value) {
    if (value > 0) {
      if (value % 2 == 0) return value * 2;
    }
    return value;
  }

  static int another(int value) {
    if (value < 0) {
      if (value % 2 != 0) return -value;
    }
    return value;
  }
}
