// The Settings sections the TUI reaches with /set, /keys, /serve, /doctor, /theme and /connect:
// every config key with a typed control, stored API keys, serve, doctor, the color theme and the
// saved servers this daemon reaches.
import { SegmentedControl } from "@zovaris/sephiro/segmented-control";
import { Select } from "@zovaris/sephiro/select";
import { SettingsList } from "@zovaris/sephiro/settings-list";
import { Toggle } from "@zovaris/sephiro/toggle";
import "@zovaris/sephiro/css/segmented-control.css";
import "@zovaris/sephiro/css/select.css";
import "@zovaris/sephiro/css/settings-list.css";
import "@zovaris/sephiro/css/toggle.css";
import "./settings.css";
import { useEffect, useState } from "preact/hooks";
import { addServer, connectTest, loadSettings, removeKey, removeServer, runDoctor, setKey, setServe, setSetting, type Settings, unsetSetting } from "../lib/actions";
import { api, currentServer, pickServer } from "../lib/auth";
import { controlOf, groupSettings, type SettingRow } from "../lib/parity";
import { statusLine, useServers } from "../components/servers";
import { saveAccent, THEMES, type ThemeName, themeOf } from "../lib/theme";
import { St } from "../components/ui";

const err = (e: unknown) => String((e as Error).message ?? e);
const GROUP: Record<string, string> = { notify: "Notifications", linear: "Linear" };

const rowId = (r: SettingRow) => "s-" + r.key.replace(/\W/g, "-");

// Control is the typed editor of one config key, with Reset once the user has set it.
function Control({ r, onSaved }: { r: SettingRow; onSaved: (msg: string) => void }) {
  const [v, setV] = useState(r.value);
  useEffect(() => setV(r.value), [r.value]);
  const c = controlOf(r);
  const save = (x: string) => setSetting(r.key, x).then(() => onSaved("saved " + r.key), (e) => onSaved("error: " + err(e)));
  const reset = () => unsetSetting(r.key).then(() => onSaved(r.key + " is back to its default"), (e) => onSaved("error: " + err(e)));
  const id = rowId(r);
  return (
    <div class="c">
      {c === "toggle" ? (
        <Toggle id={id} label={r.key} checked={(v || r.default) === "true"} onCheckedChange={(on) => (setV(String(on)), save(String(on)))} />
      ) : c === "select" ? (
        <Select id={id} ariaLabel={r.key} value={v || r.default} options={(r.options ?? []).map((o) => ({ value: o, label: o }))} onValueChange={(x) => (setV(x), save(x))} size="sm" />
      ) : c === "secret" ? (
        <span class="mono t3">{r.value || "unset"} · set it under API keys</span>
      ) : (
        <form onSubmit={(e) => (e.preventDefault(), save(v))} style="display:flex;gap:6px">
          <input id={id} type={c === "number" ? "number" : "text"} value={v} placeholder={r.default} onInput={(e) => setV(e.currentTarget.value)} spellcheck={false} />
          {v !== r.value && <button class="btn sm" type="submit">Save</button>}
        </form>
      )}
      {r.src === "user" && <button class="btn sm" title="Back to the default (/unset)" onClick={reset}>Reset</button>}
    </div>
  );
}

function Keys({ st, onSaved }: { st: Settings; onSaved: (m: string) => void }) {
  const [svc, setSvc] = useState("");
  const [key, setK] = useState("");
  const add = async (e: Event) => {
    e.preventDefault();
    try {
      await setKey(svc.trim(), key);
      setSvc("");
      setK("");
      onSaved("stored the " + svc.trim() + " key");
    } catch (x) {
      onSaved("error: " + err(x));
    }
  };
  return (
    <div class="card">
      {st.keys.map((k) => (
        <div class="kv">
          <span class="mono" style="font-weight:500">{k.service}</span>
          <span class="mono t3">{k.masked}</span>
          <span class="sp" />
          <button class="btn sm" onClick={() => removeKey(k.service).then(() => onSaved("removed the " + k.service + " key"), (e) => onSaved("error: " + err(e)))}>Remove</button>
        </div>
      ))}
      {!st.keys.length && <p class="hint" style="padding:12px">No API keys stored.</p>}
      <form class="inline" onSubmit={add}>
        <input aria-label="Service" placeholder="service (anthropic, openai, linear, telegram)" list="key-services" value={svc} onInput={(e) => setSvc(e.currentTarget.value)} spellcheck={false} />
        <datalist id="key-services">{["anthropic", "openai", "openrouter", "linear", "telegram"].map((x) => <option value={x} />)}</datalist>
        <input aria-label="Key" type="password" placeholder="key" value={key} onInput={(e) => setK(e.currentTarget.value)} autoComplete="off" />
        <button class="btn sm" type="submit" disabled={!svc.trim() || !key}>Store</button>
      </form>
    </div>
  );
}

function Servers({ onSaved }: { onSaved: (m: string) => void }) {
  const [tick, setTick] = useState(0);
  const rows = useServers(tick);
  const [f, setF] = useState({ name: "", url: "", code: "" });
  const [busy, setBusy] = useState(false);
  const cur = currentServer();
  const add = async (e: Event) => {
    e.preventDefault();
    setBusy(true);
    try {
      await addServer(f.name.trim(), f.url.trim(), f.code.trim());
      onSaved("paired with " + f.name.trim());
      setF({ name: "", url: "", code: "" });
      setTick(tick + 1);
    } catch (x) {
      onSaved("error: " + err(x));
    }
    setBusy(false);
  };
  return (
    <div class="card">
      <div class="kv">
        <St s={cur ? "idle" : "ready"} word={cur ? "" : "on"} />
        <span style="font-weight:500">This machine</span>
        <span class="sp" />
        {cur ? <button class="btn sm" onClick={() => pickServer("")}>Switch</button> : <span class="t3">this tab</span>}
      </div>
      {rows.map((r) => (
        <div class="kv">
          <St s={r.online ? "ready" : "missing"} word={r.name === cur ? "on" : r.online ? "online" : "offline"} />
          <span style="font-weight:500">{r.name}</span>
          <span class="mono t3" title={r.url}>{statusLine(r)}</span>
          <span class="sp" />
          {r.name !== cur && <button class="btn sm" disabled={!r.online} onClick={() => pickServer(r.name)}>Switch</button>}
          {!cur && <button class="btn sm" onClick={() => removeServer(r.name).then(() => (onSaved("removed " + r.name), setTick(tick + 1)), (e) => onSaved("error: " + err(e)))}>Remove</button>}
        </div>
      ))}
      {!cur && (
        <form class="inline" onSubmit={add}>
          <input aria-label="Name" placeholder="name (home)" value={f.name} onInput={(e) => setF({ ...f, name: e.currentTarget.value })} spellcheck={false} />
          <input aria-label="URL" placeholder="http://100.x.y.z:7478" value={f.url} onInput={(e) => setF({ ...f, url: e.currentTarget.value })} spellcheck={false} />
          <input aria-label="Pairing code" placeholder="code from smithy pair" value={f.code} onInput={(e) => setF({ ...f, code: e.currentTarget.value })} spellcheck={false} autoComplete="off" />
          <button class="btn sm" type="submit" disabled={busy || !f.name || !f.url || !f.code}>{busy ? "Pairing…" : "Add server"}</button>
        </form>
      )}
      <p class="hint" style="padding:0 12px 12px">Run <code>smithy pair</code> on the other machine for a one-time code; its sessions then show here and in the sidebar.</p>
    </div>
  );
}

// More is the settings beyond agents and skills; settings that need this machine's daemon token
// (keys, serve, doctor, servers) say so on another server.
export function More() {
  const [st, setSt] = useState<Settings | null>(null);
  const [e, setE] = useState("");
  const [msg, setMsg] = useState("");
  const [doc, setDoc] = useState<string[] | null>(null);
  const [docBusy, setDocBusy] = useState(false);
  const [sent, setSent] = useState<string[] | null>(null);
  const [sendBusy, setSendBusy] = useState(false);
  const testSend = async () => {
    setSendBusy(true);
    setSent(await connectTest().then((r) => [...r.results, ...(r.error ? [r.error] : [])], (x) => ["test: " + err(x)]));
    setSendBusy(false);
  };
  const [theme, setTheme] = useState<ThemeName>("steel");
  const load = () => loadSettings().then((x) => (setSt(x), setTheme(themeOf(x))), (x) => setE(err(x)));
  useEffect(() => void load(), []);
  const saved = (m: string) => (setMsg(m), load());
  const doctor = async () => {
    setDocBusy(true);
    setDoc(await runDoctor().then((r) => r.lines, (x) => ["doctor: " + err(x)]));
    setDocBusy(false);
  };
  const groups = st ? groupSettings(st.settings) : [];
  groups.sort((a, b) => (GROUP[b.group] ? 1 : 0) - (GROUP[a.group] ? 1 : 0));
  return (
    <>
      <div id="set-theme">
        <div class="lbl">Theme</div>
        <div class="card"><div class="kv">
          <SegmentedControl ariaLabel="Color theme" value={theme} onValueChange={(n) => (setTheme(n as ThemeName), saveAccent(api, n as ThemeName).then(() => setMsg("theme " + n), (x) => setMsg("error: " + err(x))))}
            options={THEMES.map((t) => ({ value: t.name, label: t.name }))} />
          <span class="t3">shared with the TUI (ui.theme)</span>
        </div></div>
      </div>
      {msg && <p class={msg.startsWith("error") ? "err" : "t3"} role="status">{msg}</p>}
      {e && !st && <p class="t3">Settings, keys, serve and doctor need this machine's daemon token: {e}</p>}
      {st && (
        <>
          <div id="set-serve">
            <div class="lbl">Serve</div>
            <div class="card"><div class="kv">
              <Toggle label="Serve" checked={st.serve} onCheckedChange={(on) => setServe(on).then(() => saved("serve " + (on ? "on" : "off")), (x) => setMsg("error: " + err(x)))} />
              <span>{st.serve ? "Serve may launch new work" : "Serve launches nothing new (auto.paused)"}</span>
            </div></div>
          </div>
          <div id="set-keys"><div class="lbl">API keys</div><Keys st={st} onSaved={saved} /></div>
          <SettingsList
            id="set-settings"
            groups={groups.map((g) => ({
              id: g.group,
              title: GROUP[g.group] ?? g.group,
              items: g.rows.map((r) => ({
                id: r.key,
                label: <label class="mono" for={rowId(r)}>{r.key}{r.src !== "default" && r.src !== "unset" && <span class="t3"> · {r.src}</span>}</label>,
                description: r.meaning + (r.default ? ` (default ${r.default})` : ""),
                control: <Control key={r.key} r={r} onSaved={saved} />,
              })),
            }))}
          />
          <div id="set-channels">
            <div class="lbl">Chat channels</div>
            <div class="card">
              <div class="kv"><span>A test message to each connector's notifications and chat channel (connect.*.notify, connect.*.chat)</span><span class="sp" /><button class="btn sm" disabled={sendBusy} onClick={testSend}>{sendBusy ? "Sending…" : "Send test"}</button></div>
              {sent && <pre class="out" style="margin:0 12px 12px">{sent.join("\n")}</pre>}
            </div>
          </div>
          <div id="set-doctor">
            <div class="lbl">Doctor</div>
            <div class="card">
              <div class="kv"><span>smithy doctor on the daemon's machine</span><span class="sp" /><button class="btn sm" disabled={docBusy} onClick={doctor}>{docBusy ? "Running…" : doc ? "Run again" : "Run"}</button></div>
              {doc && <pre class="out" style="margin:0 12px 12px">{doc.join("\n")}</pre>}
            </div>
          </div>
        </>
      )}
      <div id="set-servers"><div class="lbl">Servers</div><Servers onSaved={setMsg} /></div>
    </>
  );
}
