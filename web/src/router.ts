import { useEffect, useState } from "preact/hooks";

export type RouteId = "sessions" | "decisions" | "agents" | "board" | "bench";

export const ROUTES: { id: RouteId; label: string; key: string }[] = [
  { id: "sessions", label: "Sessions", key: "1" },
  { id: "decisions", label: "Decisions", key: "2" },
  { id: "agents", label: "Agents", key: "3" },
  { id: "board", label: "Board", key: "4" },
  { id: "bench", label: "Bench", key: "5" },
];

export function route(hash: string): RouteId {
  const id = hash.replace(/^#\/?/, "").split(/[/?&]/)[0];
  return ROUTES.find((r) => r.id === id)?.id ?? "sessions";
}

export function useRoute(): RouteId {
  const [r, set] = useState(() => route(location.hash));
  useEffect(() => {
    const on = () => set(route(location.hash));
    addEventListener("hashchange", on);
    return () => removeEventListener("hashchange", on);
  }, []);
  return r;
}
