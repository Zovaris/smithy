// Light or dark: the system's until the toggle picks one, then that choice (kept per browser).
const KEY = "smithy.mode";

export type Mode = "light" | "dark";

export const system = (): Mode => (typeof matchMedia === "function" && matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");

export function current(): Mode {
  const m = document.documentElement.dataset.mode;
  return m === "light" || m === "dark" ? m : system();
}

export function initTheme(): void {
  let m: string | null = null;
  try {
    m = localStorage.getItem(KEY);
  } catch {
    /* storage blocked: follow the system */
  }
  const q = new URLSearchParams(location.search).get("mode");
  if (q === "light" || q === "dark") m = q;
  if (m === "light" || m === "dark") document.documentElement.dataset.mode = m;
}

export function toggleTheme(): Mode {
  const next: Mode = current() === "dark" ? "light" : "dark";
  document.documentElement.dataset.mode = next;
  try {
    localStorage.setItem(KEY, next);
  } catch {
    /* the choice lasts for this page only */
  }
  return next;
}

// Color themes are ui.theme, shared with the TUI through the daemon; steel (the ember) is the default.
export type ThemeName = "steel" | "frost" | "plum";
export const THEMES: { name: ThemeName; hint: string }[] = [
  { name: "steel", hint: "graphite with the ember accent (default)" },
  { name: "frost", hint: "graphite with a cool teal accent" },
  { name: "plum", hint: "graphite with a violet accent" },
];

export function themeOf(s: { settings?: { key: string; value: string }[] } | null): ThemeName {
  const v = s?.settings?.find((r) => r.key === "ui.theme")?.value;
  return THEMES.find((t) => t.name === v)?.name ?? "steel";
}

export const accentOf = (n: ThemeName): string | undefined => (n === "steel" ? undefined : n);

export function applyAccent(n: ThemeName): void {
  const a = accentOf(n);
  if (a) document.documentElement.dataset.accent = a;
  else delete document.documentElement.dataset.accent;
}

type Api = (path: string, init?: RequestInit) => Promise<Response>;

export async function loadAccent(api: Api): Promise<ThemeName> {
  const r = await api("/v1/settings");
  const n = themeOf(r.ok ? await r.json() : null);
  applyAccent(n);
  return n;
}

export async function saveAccent(api: Api, n: ThemeName): Promise<void> {
  applyAccent(n);
  const r = await api("/v1/settings/ui.theme", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ value: n }) });
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error ?? "could not save ui.theme");
}
