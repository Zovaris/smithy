// Heights keeps one height per transcript row (estimated until measured) in a Fenwick tree, so
// the offset of any row and the window around a scroll position stay O(log n) at 10k rows.
export class Heights {
  private h: number[] = [];
  private t: number[] = [0];
  constructor(private est: number) {}

  resize(n: number): void {
    if (n === this.h.length) return;
    this.h = this.h.slice(0, n);
    while (this.h.length < n) this.h.push(this.est);
    this.t = new Array(n + 1).fill(0);
    for (let i = 0; i < n; i++) this.add(i, this.h[i]);
  }

  set(i: number, v: number): boolean {
    if (i >= this.h.length || this.h[i] === v) return false;
    this.add(i, v - this.h[i]);
    this.h[i] = v;
    return true;
  }

  private add(i: number, d: number) {
    for (let k = i + 1; k < this.t.length; k += k & -k) this.t[k] += d;
  }

  offset(i: number): number {
    let s = 0;
    for (let k = i; k > 0; k -= k & -k) s += this.t[k];
    return s;
  }

  total(): number {
    return this.offset(this.h.length);
  }

  // index is the row at pixel y.
  index(y: number): number {
    let pos = 0, rem = y;
    for (let step = 1 << Math.floor(Math.log2(this.t.length || 1)); step > 0; step >>= 1) {
      const n = pos + step;
      if (n < this.t.length && this.t[n] <= rem) {
        pos = n;
        rem -= this.t[n];
      }
    }
    return Math.min(pos, Math.max(this.h.length - 1, 0));
  }

  window(scroll: number, view: number, over: number): { from: number; to: number; top: number } {
    const n = this.h.length;
    if (!n) return { from: 0, to: 0, top: 0 };
    const from = Math.max(0, this.index(scroll) - over);
    const to = Math.min(n, this.index(scroll + Math.max(view - 1, 0)) + 1 + over);
    return { from, to, top: this.offset(from) };
  }
}
