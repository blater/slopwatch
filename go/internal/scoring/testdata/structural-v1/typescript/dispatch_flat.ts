export function dispatchFlat(kind: number, value: number): number {
  switch (kind) {
    case 0: return value + 1;
    case 1: return value * 2;
    default: return value - 1;
  }
}
