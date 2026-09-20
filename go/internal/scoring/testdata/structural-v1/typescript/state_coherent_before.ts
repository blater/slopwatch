export class CoherentBefore {
  private total = 0;

  advance(delta: number): number {
    if (delta < 0) return this.total;
    this.total += delta;
    if (this.total > 100) this.total = 100;
    return this.total;
  }

  advanceAgain(delta: number): number {
    if (delta < 0) return this.total;
    this.total += delta;
    if (this.total > 100) this.total = 100;
    return this.total;
  }
}
