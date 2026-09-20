export function distinctTwo(value: number): number {
  if (value > 0) {
    if (value % 2 === 0) return value * 2;
  }
  return value;
}

export function anotherDifficult(value: number): number {
  if (value < 0) {
    if (value % 2 !== 0) return -value;
  }
  return value;
}
