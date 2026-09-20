export class CosmeticBefore {
  private total = 0;
  private updates = 0;

  advance(delta: number): number {
    if (delta < 0) return this.total;
    this.total += delta;
    if (this.total > 100) this.total = 100;
    this.updates++;
    return this.total;
  }
}
