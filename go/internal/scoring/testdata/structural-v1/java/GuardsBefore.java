final class GuardsBefore {
  static boolean run(boolean a, boolean b) {
    if (a) {
      if (b) return true;
    }
    return false;
  }
}
