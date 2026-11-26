// The transcript detail /mode picks, as in the TUI: minimal folds each run of tools into one line,
// expandable shows a header per tool, verbose opens them all.
export type Detail = "minimal" | "expandable" | "verbose";
export const DETAILS: Detail[] = ["minimal", "expandable", "verbose"];

// nextDetail is /mode as the TUI runs it: a named mode, else the next one round.
export function nextDetail(cur: Detail, arg: string): Detail {
  const a = arg.trim().toLowerCase() as Detail;
  return DETAILS.includes(a) ? a : DETAILS[(DETAILS.indexOf(cur) + 1) % DETAILS.length];
}
