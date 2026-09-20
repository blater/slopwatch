final class DispatchFlat {
  static int run(int kind, int value) {
    switch (kind) {
      case 0: return value + 1;
      case 1: return value * 2;
      default: return value - 1;
    }
  }
}
