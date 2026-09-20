export function unusedBefore(x: number): number {
  const used = x * 2;
  if (x > 0) {
    const unused = x * 3;
    void unused;
  }
  return used + 1;
}
