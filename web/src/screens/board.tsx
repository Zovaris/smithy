import { SegmentedControl } from "@zovaris/sephiro/segmented-control";
import "@zovaris/sephiro/css/segmented-control.css";
import { TaskBoard as Tasks } from "@zovaris/sephiro/task-board";
import "@zovaris/sephiro/css/task-board.css";
import { useEffect, useState } from "preact/hooks";
import { api } from "../lib/auth";
import { blocked, COLUMNS, loadTasks, moved, moveTask, rel, type TaskBoard, type TaskCard, type TaskList } from "../lib/tasks";

type Project = { dir: string; name: string };

// Board is a project's local task files (`smithy tasks`) as columns by status. Drag a card, or
// Shift+Arrow on it, to move it; the move writes the file's Status line through the daemon.
export default function Board() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [project, setProject] = useState("");
  const [board, setBoard] = useState<TaskBoard | null>(null);
  const [list, setList] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    api("/v1/projects").then((r) => (r.ok ? r.json() : null)).then((l) => l && setProjects(l.projects ?? [])).catch(() => {});
  }, []);
  useEffect(() => {
    setErr("");
    loadTasks(project).then((b) => {
      setBoard(b);
      setList((cur) => (b.lists.some((l) => l.dir === cur) ? cur : b.lists[0]?.dir ?? ""));
    }).catch((e) => setErr(String(e.message ?? e)));
  }, [project]);

  const move = async (t: TaskCard, status: string) => {
    if (!board || !status || status === t.status) return;
    const before = board;
    setBoard(moved(board, t.file, status));
    setErr("");
    try {
      await moveTask(t.file, status);
    } catch (e) {
      setBoard(before);
      setErr(`${t.nn} not moved: ${(e as Error).message}`);
    }
  };

  if (!board) return <section class="page"><p class="t3">{err || "Reading the task files…"}</p></section>;
  const cur: TaskList | undefined = board.lists.find((l) => l.dir === list);
  const pname = projects.find((p) => p.dir === board.project)?.name ?? board.project.split("/").pop();
  return (
    <section class="page wide" aria-label="Board">
      <header>
        <h2>Board</h2>
        <p>
          {!board.project ? "No project yet. Add one from New session or Settings." : cur ? `${cur.tasks.length} task${cur.tasks.length === 1 ? "" : "s"} in ${pname} / ${cur.name}` : `No task files in ${pname}.`}
        </p>
      </header>
      {(projects.length > 1 || board.lists.length > 1) && (
        <div class="seg-row">
          {projects.length > 1 && (
            <SegmentedControl ariaLabel="Project" size="sm" value={board.project} onValueChange={setProject} options={projects.map((p) => ({ value: p.dir, label: p.name }))} />
          )}
          {board.lists.length > 1 && (
            <SegmentedControl ariaLabel="Task list" size="sm" value={list} onValueChange={setList} options={board.lists.map((l) => ({ value: l.dir, label: l.name }))} />
          )}
        </div>
      )}
      {err && <p class="err" role="alert">{err}</p>}
      {!cur ? (
        <div class="card"><p class="hint" style="padding:16px">
          The Board shows the Markdown task files <code>smithy tasks</code> reads: <code>docs/specs/&lt;list&gt;/NN-slug.md</code>, <code>.docs/specs/</code> or <code>~/.smithy/plans/&lt;repo&gt;/</code>. Make one with <code>smithy tasks add "&lt;title&gt;"</code>.
        </p></div>
      ) : (
        <Tasks
          label={`Tasks in ${cur.name}`}
          columns={COLUMNS}
          tasks={cur.tasks.map((t) => ({
            id: t.file,
            column: t.status,
            title: `${t.nn} · ${t.title}`,
            path: rel(t.file, board.project),
            tags: [...(t.blockers.length ? ["after " + t.blockers.join(", ")] : []), ...(t.pr ? ["PR"] : [])],
            status: t.status === "doing" ? "running" : blocked(cur, t) ? "attention" : undefined,
            statusLabel: t.status === "doing" ? "In progress" : blocked(cur, t) ? "Blocked" : undefined,
          }))}
          onOpen={(id) => { const t = cur.tasks.find((x) => x.file === id); if (t?.pr) open(t.pr, "_blank", "noreferrer"); }}
          onMove={(id, col) => { const t = cur.tasks.find((x) => x.file === id); if (t) move(t, col); }}
        />
      )}
    </section>
  );
}
