export function indexed(values: number[], queries: number[]): number {
  const index = new Map<number, number>();
  values.forEach((value, i) => index.set(value, i));
  let found = 0;
  queries.forEach(query => { found += index.get(query) ?? 0; });
  return found;
}
