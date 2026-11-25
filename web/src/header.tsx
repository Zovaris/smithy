// Half-block header: a static ▀ gradient on the steel ramp with one ember spark, dithered with the
// 4x4 Bayer matrix. It never animates in the shell: motion is for running work only.
const B = [[0, 8, 2, 10], [12, 4, 14, 6], [3, 11, 1, 9], [15, 7, 13, 5]];
const RAMP = ["#0E1116", "#131820", "#1A212B", "#222B37", "#2C3746", "#3B4A5E"];
const COLS = 160;

function cell(x: number, y: number): string {
  const v = 0.35 + 0.3 * Math.sin(x * 0.09 + y * 0.7) + 0.15 * Math.sin(x * 0.031);
  if (y === 1 && Math.abs(x - 24) < 2) return "#F2762E";
  const n = RAMP.length - 1, s = Math.max(0, Math.min(1, v)) * n, i = Math.floor(s);
  return RAMP[Math.min(n, i + (s - i > (B[y & 3][x & 3] + 0.5) / 16 ? 1 : 0))];
}

const ROWS = [0, 1].map((r) =>
  Array.from({ length: COLS }, (_, x) => `<span style="color:${cell(x, r * 2)};background:${cell(x, r * 2 + 1)}">▀</span>`).join(""),
).join("<br>");

export function Header() {
  return <div class="hdr" aria-hidden="true" dangerouslySetInnerHTML={{ __html: ROWS }} />;
}
