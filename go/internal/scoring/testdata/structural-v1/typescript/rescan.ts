export function rescan(values: number[], queries: number[]): number {
  let found = 0;
  for (const query of queries) {
    for (let i = 0; i < values.length; i++) {
      if (values[i] === query) { found += i; break; }
    }
  }
  return found;
}
