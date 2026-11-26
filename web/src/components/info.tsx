import { Dialog } from "@zovaris/sephiro/dialog";
import "@zovaris/sephiro/css/dialog.css";

export type Info = { title: string; lines: string[] };

// InfoPanel is the TUI's info overlay: /help, /log and /remote. A line starting with # is a
// heading; a tab splits a line into what to run and what it does.
export default function InfoPanel({ info, onClose }: { info: Info; onClose: () => void }) {
  return (
    <Dialog open onClose={onClose} title={info.title} keys={[{ key: "esc", label: "close" }]}>
      <div class="info">
        {info.lines.map((l) => {
          if (l.startsWith("#")) return <div class="lbl">{l.slice(1)}</div>;
          const [k, d] = l.split("\t");
          return <div class="top2"><span class="mono">{k}</span>{d && <span class="t3">{d}</span>}</div>;
        })}
      </div>
    </Dialog>
  );
}
