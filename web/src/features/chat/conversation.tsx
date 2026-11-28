import type { ComponentChildren } from "preact";
import { lazy, Suspense } from "preact/compat";
import { useLayoutEffect, useRef } from "preact/hooks";
import { I, toolIcon } from "../../components/icons";
import { prose, agentName, argLine, initials, type Dec, type Item, type Sess, type Tool } from "../../lib/store";
import { Md } from "../../components/md";
import { St } from "../../components/ui";
import type { Detail } from "../../lib/detail";

// The question card loads on the first question, off the first-load budget (ADR 0011).
const Card = lazy(() => import("../../components/decision"));
const Decision = ({ d }: { d: Dec }) => <Suspense fallback={null}><Card d={d} /></Suspense>;

const cost = (s: Sess) => (s.usage?.cost_usd != null ? " · $" + s.usage.cost_usd.toFixed(2) : "");

// Tools is a run of tool calls in the /mode detail: minimal folds the run into one line, expandable
// shows a header per tool, verbose opens every one.
function Tools({ tools, detail }: { tools: Tool[]; detail: Detail }) {
  if (detail === "minimal")
    return (
      <details class="tool">
        <summary><I n="term" /><span class="k">{tools.length} tools</span><span class="a">{tools.map((t) => t.name).join(", ")}</span></summary>
        <Tools tools={tools} detail="expandable" />
      </details>
    );
  return (
    <div class="tools">
      {tools.map((t) => (
        <details class={"tool" + (t.is_error ? " err" : "")} open={detail === "verbose"}>
          <summary>
            <I n={toolIcon(t.name)} />
            <span class="k">{t.name}</span>
            <span class="a">{argLine(t)}</span>
            {t.is_error && <span class="st fail">failed</span>}
          </summary>
          {t.output ? <pre>{t.output}</pre> : <pre class="t3">no output</pre>}
        </details>
      ))}
    </div>
  );
}

// Conversation is one session's turns: your messages on the right, the agent's text, tool calls
// (folded, open on click) and decisions inline, and a live "working" line while a turn runs.
// loose are open decisions no session claims (the command guard asks through the daemon with no
// session id); they show in every session that is working, since any of them may have asked.
export function Conversation({ s, items, decisions, loose, extra, detail = "expandable" }: { s: Sess; items: Item[]; decisions: Record<string, Dec>; loose: Dec[]; extra?: ComponentChildren; detail?: Detail }) {
  const box = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  useLayoutEffect(() => {
    const b = box.current;
    if (b && stick.current) b.scrollTop = b.scrollHeight;
  });
  const onScroll = () => {
    const b = box.current!;
    stick.current = b.scrollHeight - b.scrollTop - b.clientHeight < 48;
  };
  const who = (
    <div class="who">
      {agentName(s.agent)} <span class="mono">{(s.model || "default model") + cost(s)}</span>
    </div>
  );
  const first = items.findIndex((i) => i.kind === "agent");
  return (
    <div class="scroll" ref={box} onScroll={onScroll} role="log" aria-label="Conversation" aria-live="polite" tabIndex={0}>
      <div class="conv">
        {!items.length && <p class="t3">The session is ready. Send the first message below.</p>}
        {items.map((it, k) =>
          it.kind === "you" ? (
            <div class="you">{it.text}</div>
          ) : (
            <div class="agent">
              <div class="av" aria-hidden="true">{initials(s.agent)}</div>
              <div class="body">
                {k === first && who}
                {it.parts.map((p) =>
                  p.kind === "text" ? prose(p.text).trim() ? <Md text={prose(p.text).trim()} /> : null : p.kind === "tools" ? <Tools tools={p.tools} detail={detail} /> : decisions[p.id] ? <Decision d={decisions[p.id]} /> : null,
                )}
              </div>
            </div>
          ),
        )}
        {extra}
        {loose.map((d) => (
          <div class="agent">
            <div class="av" aria-hidden="true">{initials(s.agent)}</div>
            <div class="body"><Decision d={d} /></div>
          </div>
        ))}
        {s.status === "working" && !loose.length && (
          <div class="agent">
            <div class="av" aria-hidden="true">{initials(s.agent)}</div>
            <div class="body"><span class="thinking"><St s="working" /></span></div>
          </div>
        )}
        {s.status === "failed" && s.detail && (
          <p class="err" role="alert">The turn failed: {s.detail}</p>
        )}
      </div>
    </div>
  );
}
