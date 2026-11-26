// A unified `git diff` split into files with their hunks, for the Changes rail.
export type Line = { t: "hunk" | "add" | "del" | "ctx"; text: string; n?: number };
export type File = { path: string; add: number; del: number; lines: Line[] };

export function parse(patch: string): File[] {
  const out: File[] = [];
  let f: File | null = null;
  let oldN = 0, newN = 0;
  for (const l of patch.split("\n")) {
    if (l.startsWith("diff --git ")) {
      const m = / b\/(.+)$/.exec(l);
      f = { path: m ? m[1] : l.slice(11), add: 0, del: 0, lines: [] };
      out.push(f);
      continue;
    }
    if (!f) continue;
    if (l.startsWith("+++ ")) {
      if (l !== "+++ /dev/null") f.path = l.slice(4).replace(/^b\//, "");
      continue;
    }
    if (l.startsWith("--- ") || l.startsWith("index ") || /^(new|deleted) file mode|^similarity|^rename |^old mode|^new mode|^Binary/.test(l)) continue;
    const h = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/.exec(l);
    if (h) {
      oldN = +h[1];
      newN = +h[2];
      f.lines.push({ t: "hunk", text: l });
    } else if (l.startsWith("+")) {
      f.add++;
      f.lines.push({ t: "add", text: l, n: newN++ });
    } else if (l.startsWith("-")) {
      f.del++;
      f.lines.push({ t: "del", text: l, n: oldN++ });
    } else if (l.startsWith(" ")) {
      f.lines.push({ t: "ctx", text: l, n: newN++ });
      oldN++;
    }
  }
  return out;
}

export const totals = (fs: File[]) => fs.reduce((a, f) => ({ add: a.add + f.add, del: a.del + f.del }), { add: 0, del: 0 });

// short keeps the last two path segments, the part that tells files apart in a narrow rail.
export const short = (p: string) => p.split("/").slice(-2).join("/");

export type Comment = { path: string; line: number; text: string };

// withComments puts the comments made on the diff in front of the next message.
export function withComments(msg: string, cs: Comment[]): string {
  if (!cs.length) return msg;
  const notes = cs.map((c) => `- ${c.path}:${c.line}: ${c.text}`).join("\n");
  return `Comments on the diff:\n${notes}` + (msg.trim() ? `\n\n${msg}` : "");
}
