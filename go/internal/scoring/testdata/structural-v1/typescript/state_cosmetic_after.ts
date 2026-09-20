export class CosmeticAfter {
  private total = 0;
  private updates = 0;

  private acceptState(delta: number): boolean {
    if (delta < 0) return false;
    return true;
  }
  private setState(delta: number): void {
    this.total += delta;
    if (this.total > 100) this.total = 100;
  }
  private markState(): void { this.updates++; }

  advance(delta: number): number {
    if (!this.acceptState(delta)) return this.total;
    this.setState(delta);
    this.markState();
    return this.total;
  }
}
