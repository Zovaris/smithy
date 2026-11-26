import type { ComponentChildren } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { action, suggest, type Cmd } from "./commands";
import { I } from "./icons";

type Props = {
  placeholder: string;
  skills: string[];
  working?: boolean;
  canSend?: boolean;
  draft?: string;
  pills?: ComponentChildren;
  above?: ComponentChildren;
  onSend: (text: string) => Promise<string>;
  onAction: (name: string) => void;
  onStop?: () => void;
};

// Composer is the one input: ↵ sends, ⇧↵ breaks the line, `/` lists smithy's commands and the
// installed skills. A smithy command alone runs here; anything else goes to the agent.
export function Composer({ placeholder, skills, working, canSend = true, draft, pills, above, onSend, onAction, onStop }: Props) {
  const [text, setText] = useState("");
  const [sel, setSel] = useState(0);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [menu, setMenu] = useState<"" | "skills">("");
  const ta = useRef<HTMLTextAreaElement>(null);
  const list: Cmd[] = menu === "skills" ? skills.map((s) => ({ name: s, hint: "skill", kind: "skill" })) : suggest(text, skills);

  useEffect(() => {
    if (draft != null) {
      setText(draft);
      ta.current?.focus();
    }
  }, [draft]);
  useLayoutEffect(() => {
    const t = ta.current;
    if (!t) return;
    t.style.height = "auto";
    t.style.height = t.scrollHeight + "px";
  }, [text]);
  useEffect(() => setSel(0), [list.length, menu]);
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      const a = document.activeElement;
      if (e.key === "/" && !e.metaKey && !e.ctrlKey && (!a || a === document.body)) {
        e.preventDefault();
        ta.current?.focus();
        setText("/");
      }
    };
    addEventListener("keydown", on);
    return () => removeEventListener("keydown", on);
  }, []);

  const pick = (c: Cmd) => {
    setMenu("");
    if (c.kind === "action") {
      setText("");
      onAction(c.name);
      return;
    }
    setText("/" + c.name + " ");
    ta.current?.focus();
  };

  const submit = async () => {
    const t = text.trim();
    if (!t || busy || !canSend) return;
    const a = action(t);
    if (a) {
      setText("");
      onAction(a);
      return;
    }
    setBusy(true);
    setErr("");
    const e = await onSend(t).catch((x) => String((x as Error).message ?? x));
    setBusy(false);
    if (e) setErr(e);
    else setText("");
  };

  const onKey = (e: KeyboardEvent) => {
    if (list.length) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setSel((s) => (s + (e.key === "ArrowDown" ? 1 : list.length - 1)) % list.length);
        return;
      }
      if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) {
        e.preventDefault();
        pick(list[sel]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setMenu("");
        if (text.startsWith("/")) setText("");
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      submit();
    } else if (e.key === "Escape" && working && onStop) {
      e.preventDefault();
      onStop();
    }
  };

  return (
    <div class="composer-wrap">
      {!!list.length && (
        <div class="menu" role="listbox" aria-label={menu === "skills" ? "Skills" : "Commands"}>
          {menu === "skills" && <h4>Start the message with a skill</h4>}
          {list.map((c, i) => (
            <button role="option" aria-selected={i === sel} onMouseDown={(e) => e.preventDefault()} onClick={() => pick(c)}>
              <span class="mono">/{c.name}</span>
              <span class="t3">{c.hint}</span>
            </button>
          ))}
        </div>
      )}
      <div class="composer">
        {above}
        <label class="sr" for="msg">Message</label>
        <textarea
          id="msg"
          ref={ta}
          rows={1}
          value={text}
          placeholder={placeholder}
          onInput={(e) => setText(e.currentTarget.value)}
          onKeyDown={onKey}
          enterkeyhint="send"
          autoComplete="off"
        />
        <div class="row">
          {pills}
          {!!skills.length && (
            <button class="pill" aria-haspopup="listbox" aria-expanded={menu === "skills"} onClick={() => setMenu(menu ? "" : "skills")}>
              <I n="layers" />
              <span class="hide-s">Skills</span>
            </button>
          )}
          <span class="sp" />
          <span class="hints" aria-hidden="true"><kbd>/</kbd> commands</span>
          {working && onStop && !text.trim() ? (
            <button class="send stop" aria-label="Stop" title="Stop (esc)" onClick={onStop}>
              <svg class="i" viewBox="0 0 24 24" aria-hidden="true"><rect x="7" y="7" width="10" height="10" fill="currentColor" stroke="none" /></svg>
            </button>
          ) : (
            <button class="send" aria-label="Send" disabled={!text.trim() || busy || !canSend} onClick={submit}>
              <I n="up" />
            </button>
          )}
        </div>
        {err && <p class="err" role="alert">{err}</p>}
      </div>
    </div>
  );
}
