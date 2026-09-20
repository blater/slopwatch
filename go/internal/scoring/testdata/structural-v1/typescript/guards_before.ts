export function guardsBefore(a: boolean, b: boolean): boolean {
  if (a) {
    if (b) return true;
  }
  return false;
}
