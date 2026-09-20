final class Rescan {
  static int run(int[] values, int[] queries) {
    int found = 0;
    for (int query : queries) {
      for (int i = 0; i < values.length; i++) {
        if (values[i] == query) {
          found += i;
          break;
        }
      }
    }
    return found;
  }
}
