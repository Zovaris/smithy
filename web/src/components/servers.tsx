// The server switcher: this machine plus every saved server (smithy servers), each with its
// status, ready agents and sessions. Picking one points this tab's API at it through the
// daemon's /v1/servers proxy; the choice lasts for the tab.
import { useEffect, useState } from "preact/hooks";
import { api, currentServer, pickServer } from "../lib/auth";
import { I } from "./icons";
import { agentName, type Status } from "../lib/store";
import { St } from "./ui";

export type ServerRow = {
  name: string;
  url: string;
  online: boolean;
  version?: string;
  agents: string[];
  sessions: { id: string; title: string; status: Status; agent: string }[];
  error?: string;
};

export function statusLine(s: ServerRow): string {
  if (!s.online) return "offline";
  const agents = s.agents.length ? s.agents.map(agentName).join(", ") : "no ready agent";
  return `${s.version ?? ""} · ${agents}`;
}

export function useServers(tick = 0): ServerRow[] {
  const [rows, set] = useState<ServerRow[]>([]);
  useEffect(() => {
    let on = true;
    const load = () =>
      api("/v1/servers")
        .then((r) => (r.ok ? r.json() : { servers: [] }))
        .then((x: { servers: ServerRow[] }) => on && set(x.servers ?? []))
        .catch(() => {});
    load();
    const t = setInterval(load, 30000);
    return () => ((on = false), clearInterval(t));
  }, [tick]);
  return rows;
}

// Servers is the sidebar group of the servers this tab is not on, sessions under each.
export function Servers({ rows }: { rows: ServerRow[] }) {
  const cur = currentServer();
  const others = rows.filter((r) => r.name !== cur);
  return (
    <>
      {cur && (
        <div class="grp">
          <h4><I n="server" sm />This machine<span class="sp" /></h4>
          <button class="sess srv" onClick={() => pickServer("")}><span class="t">Switch back</span><span class="s">local daemon</span></button>
        </div>
      )}
      {others.map((r) => (
        <div class="grp" aria-label={`Server ${r.name}`}>
          <h4 title={r.url}>
            <I n="server" sm />{r.name}<span class="sp" />
            <span class={"st " + (r.online ? "done" : "need")}>{r.online ? "online" : "offline"}</span>
          </h4>
          <button class="sess srv" onClick={() => pickServer(r.name)} disabled={!r.online} title={r.url}>
            <span class="t">Switch to {r.name}</span>
            <span class="s">{statusLine(r)}</span>
          </button>
          {r.sessions.map((s) => (
            <button class="sess" onClick={() => pickServer(r.name, `#/s/${encodeURIComponent(s.id)}`)}>
              <span class="t">{s.title || s.id}</span>
              <span class="s"><St s={s.status} /><span>{agentName(s.agent)}</span></span>
            </button>
          ))}
        </div>
      ))}
    </>
  );
}
