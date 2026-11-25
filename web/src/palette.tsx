import { CommandPalette, type CommandItem } from "@zovaris/sephiro/command-palette";
import "@zovaris/sephiro/css/command-palette.css";
import { glyph, MODES } from "./model";
import { live, setMode } from "./live";
import { ROUTES } from "./router";

type Item = CommandItem & { run: () => void };

// items mirrors the TUI palette: waiting on you, actions, sessions, settings.
export function items(): Item[] {
  const l = live();
  const go = (h: string) => () => void (location.hash = h);
  const cycle = () => setMode(MODES[(MODES.indexOf(l.mode) + 1) % 3]);
  return [
    ...l.pending.map((d) => ({ id: "d:" + d.id, group: "Waiting on you", icon: "?", label: d.question, hint: (d.options ?? []).join(" · "), run: go("#/decisions") })),
    { id: "a:mode", group: "Actions", icon: "≡", label: "Verbosity: " + l.mode, hint: "minimal · expandable · verbose", shortcut: ["v"], run: cycle },
    ...ROUTES.map((r) => ({ id: "r:" + r.id, group: "Actions", icon: "→", label: "Go to " + r.label, shortcut: ["alt", r.key], run: go("#/" + r.id) })),
    ...l.sessions.map((s) => ({ id: "s:" + s.id, group: "Sessions", icon: glyph(s.state), label: s.id, hint: s.kind, run: go("#/sessions?s=" + encodeURIComponent(s.id)) })),
    { id: "set:mode", group: "Settings", icon: "·", label: "chat.mode", hint: l.mode, run: cycle },
    { id: "set:daemon", group: "Settings", icon: "·", label: "daemon", hint: location.host, run: () => {} },
  ];
}

export default function Palette({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const all = items();
  return (
    <CommandPalette
      open={open}
      onOpenChange={onOpenChange}
      items={all}
      hotkey={false}
      placeholder="Search actions, sessions and settings"
      ariaLabel="Command palette"
      onSelect={(it) => {
        all.find((x) => x.id === it.id)?.run();
        onOpenChange(false);
      }}
    />
  );
}
