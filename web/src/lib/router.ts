import { useEffect, useState } from "preact/hooks";

// A view is what the center column shows: a session's conversation, the new-session page, or one
// of the overview pages (agents, board, decisions, setup, bench).
export type Page = "agents" | "board" | "decisions" | "setup" | "bench" | "workflows" | "schedules";
export type View = { name: "session"; id: string } | { name: "new" } | { name: Page };

export const PAGES: { id: Page; label: string; key: string }[] = [
  { id: "decisions", label: "Decisions", key: "2" },
  { id: "agents", label: "Agents", key: "3" },
  { id: "board", label: "Board", key: "4" },
  { id: "bench", label: "Bench", key: "5" },
  { id: "setup", label: "Settings", key: "6" },
  { id: "workflows", label: "Workflows", key: "7" },
  { id: "schedules", label: "Schedules", key: "8" },
];

// Old links (#/sessions?s=<id>) keep working.
export function route(hash: string): View {
  const h = hash.replace(/^#\/?/, "");
  const [head, ...rest] = h.split("/");
  const name = head.split("?")[0];
  if (name === "s" && rest[0]) return { name: "session", id: decodeURIComponent(rest.join("/")) };
  if (name === "sessions") {
    const s = new URLSearchParams(h.split("?")[1] ?? "").get("s");
    return s ? { name: "session", id: s } : { name: "new" };
  }
  const p = PAGES.find((x) => x.id === name);
  return p ? { name: p.id } : { name: "new" };
}

export const href = (v: View) => (v.name === "session" ? "#/s/" + encodeURIComponent(v.id) : "#/" + v.name);
export const go = (v: View) => void (location.hash = href(v));

export function useView(): View {
  const [v, set] = useState(() => route(location.hash));
  useEffect(() => {
    const on = () => set(route(location.hash));
    addEventListener("hashchange", on);
    return () => removeEventListener("hashchange", on);
  }, []);
  return v;
}
