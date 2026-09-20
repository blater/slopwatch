import java.util.HashMap;
import java.util.Map;

final class Indexed {
  static int run(int[] values, int[] queries) {
    Map<Integer, Integer> index = new HashMap<>();
    for (int i = 0; i < values.length; i++) index.put(values[i], i);
    int found = 0;
    for (int query : queries) found += index.getOrDefault(query, 0);
    return found;
  }
}
