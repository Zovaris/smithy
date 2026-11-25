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
