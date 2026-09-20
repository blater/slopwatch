export class CoherentAfter {
  private total = 0;

  private advanceState(delta: number): number {
    if (delta < 0) return this.total;
    this.total += delta;
    if (this.total > 100) this.total = 100;
    return this.total;
  }

  advance(delta: number): number { return this.advanceState(delta); }
  advanceAgain(delta: number): number { return this.advanceState(delta); }
}
