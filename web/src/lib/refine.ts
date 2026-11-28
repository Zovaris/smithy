import type { Item } from "./store";

// refined is the prompt the refine skill ends with (a ```refined block in the last agent reply),
// "" when the conversation's last turn holds none: the harness then offers copy or a new session.
export function refined(items: Item[]): string {
  const last = items[items.length - 1];
  if (!last || last.kind !== "agent") return "";
  const text = last.parts.map((p) => (p.kind === "text" ? p.text : "")).join("\n");
  const i = text.lastIndexOf("```refined\n");
  if (i < 0) return "";
  const body = text.slice(i + "```refined\n".length);
  const j = body.indexOf("\n```");
  return (j < 0 ? body : body.slice(0, j)).trim();
}
