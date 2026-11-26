import type { ComponentChildren } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { suggest, type Cmd } from "../../lib/commands";
import { intent, withImages } from "../../lib/parity";
import { I } from "../../components/icons";

type Props = {
  placeholder: string;
  skills: string[];
  working?: boolean;
  canSend?: boolean;
  draft?: string;
  pills?: ComponentChildren;
  above?: ComponentChildren;
  onSend: (text: string) => Promise<string>;
  onAction: (name: string, arg?: string) => void;
  onStop?: () => void;
  onShell?: (cmd: string) => Promise<void>;
  onImage?: (f: Blob) => Promise<string>;
};

// Composer is the one input: ↵ sends, ⇧↵ breaks the line, `/` lists smithy's commands and the
// installed skills. A smithy command runs here, `!cmd` runs in the session folder and pasted or
// dropped images go with the next message; anything else goes to the agent.
export function Composer({ placeholder, skills, working, canSend = true, draft, pills, above, onSend, onAction, onStop, onShell, onImage }: Props) {
  const [imgs, setImgs] = useState<string[]>([]);
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
    if ((!t && !imgs.length) || busy || !canSend) return;
    const it = intent(t);
    if (it.kind === "action") {
      setText("");
      onAction(it.name, it.arg);
      return;
    }
    if (it.kind === "shell" && onShell) {
      setText("");
      setBusy(true);
      await onShell(it.cmd).catch((x) => setErr(String((x as Error).message ?? x)));
      setBusy(false);
      return;
    }
    setBusy(true);
    setErr("");
    const e = await onSend(withImages(t, imgs)).catch((x) => String((x as Error).message ?? x));
    setBusy(false);
    if (e) setErr(e);
    else {
      setText("");
      setImgs([]);
    }
  };

  const attach = async (files: Blob[]) => {
    if (!onImage) return;
    for (const f of files.filter((f) => f.type.startsWith("image/"))) {
      try {
        const p = await onImage(f);
        setImgs((l) => [...l, p]);
      } catch (x) {
        setErr("paste failed: " + String((x as Error).message ?? x));
      }
    }
  };
  const onPaste = (e: ClipboardEvent) => {
    const fs = [...(e.clipboardData?.files ?? [])].filter((f) => f.type.startsWith("image/"));
    if (fs.length && onImage) {
      e.preventDefault();
      attach(fs);
    }
  };
  const onDrop = (e: DragEvent) => {
    const fs = [...(e.dataTransfer?.files ?? [])];
    if (fs.length && onImage) {
      e.preventDefault();
      attach(fs);
    }
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
    } else if (e.key === "Escape" && imgs.length && !text) {
      e.preventDefault();
      setImgs([]);
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
      <div class={"composer" + (text.trimStart().startsWith("!") ? " bang" : "")} onDrop={onDrop} onDragOver={(e) => onImage && e.preventDefault()}>
        {above}
        {!!imgs.length && (
          <div class="cmts">
            {imgs.map((p, i) => (
              <span class="tag" title={p}>
                <I n="file" />image {i + 1}
                <button class="icon s" aria-label={"Remove image " + (i + 1)} onClick={() => setImgs(imgs.filter((x) => x !== p))}>×</button>
              </span>
            ))}
          </div>
        )}
        <label class="sr" for="msg">Message</label>
        <textarea
          id="msg"
          ref={ta}
          rows={1}
          value={text}
          placeholder={placeholder}
          onInput={(e) => setText(e.currentTarget.value)}
          onKeyDown={onKey}
          onPaste={onPaste}
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
          <span class="hints" aria-hidden="true"><kbd>/</kbd> commands{onShell && <> · <kbd>!</kbd> shell</>}</span>
          {working && onStop && !text.trim() ? (
            <button class="send stop" aria-label="Stop" title="Stop (esc)" onClick={onStop}>
              <svg class="i" viewBox="0 0 24 24" aria-hidden="true"><rect x="7" y="7" width="10" height="10" fill="currentColor" stroke="none" /></svg>
            </button>
          ) : (
            <button class="send" aria-label="Send" disabled={(!text.trim() && !imgs.length) || busy || !canSend} onClick={submit}>
              <I n="up" />
            </button>
          )}
        </div>
        {err && <p class="err" role="alert">{err}</p>}
      </div>
    </div>
  );
}
