export function relocatedRoutine(value: number): number {
  if (value < 0) return -value;
  return value * 2;
}
