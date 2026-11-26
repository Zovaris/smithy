import { CommandPalette, type CommandItem } from "@zovaris/sephiro/command-palette";
import "@zovaris/sephiro/css/command-palette.css";
import { ACTIONS } from "./commands";
import { live } from "./live";
import { go, PAGES } from "./router";
import { open, sessions, STATUS_WORD } from "./store";
import { THEMES } from "./theme";
import { split } from "./ui";

type Item = CommandItem & { run: () => void };

// items mirrors the TUI palette: what waits on you, actions, pages, sessions.
export function items(onAction: (a: string) => void, theme = "steel"): Item[] {
  const l = live();
  return [
    ...open(l.store).map((d) => ({
      id: "d:" + d.id, group: "Waiting on you", icon: "?", label: split(d.question).code || d.question, hint: d.options.join(" · "),
      run: () => (d.session ? go({ name: "session", id: d.session }) : go({ name: "decisions" })),
    })),
    ...ACTIONS.filter((a) => !PAGES.some((p) => p.id === a.name) && a.name !== "settings").map((a) => ({ id: "a:" + a.name, group: "Actions", icon: "/", label: a.hint.replace(/^\w/, (c) => c.toUpperCase()), hint: "/" + a.name, run: () => onAction(a.name) })),
    ...THEMES.map((t) => ({ id: "t:" + t.name, group: "Theme", icon: t.name === theme ? "●" : "○", label: "Theme: " + t.name, hint: t.hint, run: () => onAction("theme:" + t.name) })),
    ...PAGES.map((p) => ({ id: "p:" + p.id, group: "Go to", icon: "→", label: p.label, shortcut: ["alt", p.key], run: () => go({ name: p.id }) })),
    ...sessions(l.store).map((s) => ({ id: "s:" + s.id, group: "Sessions", icon: "·", label: s.title, hint: STATUS_WORD[s.status], run: () => go({ name: "session", id: s.id }) })),
  ];
}

export default function Palette({ open: isOpen, onOpenChange, onAction, theme }: { open: boolean; onOpenChange: (o: boolean) => void; onAction: (a: string) => void; theme?: string }) {
  const all = items(onAction, theme);
  return (
    <CommandPalette
      open={isOpen}
      onOpenChange={onOpenChange}
      items={all}
      hotkey={false}
      placeholder="Search actions, themes, pages and sessions"
      ariaLabel="Command palette"
      onSelect={(it) => {
        all.find((x) => x.id === it.id)?.run();
        onOpenChange(false);
      }}
    />
  );
}
