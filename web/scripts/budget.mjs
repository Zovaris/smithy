// Fails when the gzipped JS+CSS loaded on first visit (index.html's scripts, preloads and styles)
// is above the budget. Lazy screen chunks are not counted: they load when their route opens.
import { readFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import { join } from "node:path";

const BUDGET = 30 * 1024;
const dist = new URL("../../internal/web/dist/", import.meta.url).pathname;
const html = readFileSync(join(dist, "index.html"), "utf8");
const refs = [...html.matchAll(/(?:src|href)="\/?(assets\/[^"]+\.(?:js|css))"/g)].map((m) => m[1]);
let total = 0;
for (const r of new Set(refs)) {
  const n = gzipSync(readFileSync(join(dist, r)), { level: 9 }).length;
  total += n;
  console.log(`${(n / 1024).toFixed(1).padStart(7)} KB  ${r}`);
}
console.log(`${(total / 1024).toFixed(1).padStart(7)} KB  first load (budget ${BUDGET / 1024} KB gzip)`);
if (total > BUDGET) {
  console.error("over budget");
  process.exit(1);
}
