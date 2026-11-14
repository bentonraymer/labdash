import { useEffect, useState, type FormEvent } from "react";
import clsx from "clsx";
import { Bell, Check, Plus, Target, Trash2, X } from "lucide-react";
import { useAddChecklist, useCreateTodo, useDeleteChecklist, useDeleteTodo, useHosts, useLaunchpad, useUpdateChecklist, useUpdateTodo, type TodoPatch } from "@/lib/queries";
import type { Todo, TodoList, TodoStatus } from "@/lib/types";
import { priorities } from "@/lib/planning";
import { fmtDateTime } from "@/lib/time";
import { MarkdownPreview } from "@/components/MarkdownPreview";

const statuses: { id: TodoStatus; label: string }[] = [
  { id: "open", label: "To do" },
  { id: "doing", label: "In progress" },
  { id: "done", label: "Done" },
];

/** Side panel to edit one todo: everything saves as you go. */
export function TodoDetail({ todo: t, todos, lists, onClose }: { todo: Todo; todos: Todo[]; lists: TodoList[]; onClose: () => void }) {
  const update = useUpdateTodo();
  const del = useDeleteTodo();
  const { data: lp } = useLaunchpad();
  const { data: hosts = [] } = useHosts();
  const [title, setTitle] = useState(t.title);
  const [notes, setNotes] = useState(t.notes);
  const [editNotes, setEditNotes] = useState(!t.notes);
  useEffect(() => setTitle(t.title), [t.title]);

  const goals = todos.filter((g) => g.kind === "goal" && g.id !== t.id);
  const tasks = todos.filter((c) => c.parentId === t.id).sort((a, b) => Number(a.status === "done") - Number(b.status === "done") || a.priority - b.priority);
  const patch = (p: TodoPatch) => update.mutate({ id: t.id, ...p });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && !(e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLInputElement) && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <aside className="fixed top-0 right-0 z-30 flex h-full w-[min(420px,100vw)] shrink-0 flex-col border-l border-line bg-panel shadow-2xl shadow-black/60 xl:sticky xl:z-auto xl:h-[calc(100vh-73px)] xl:shadow-none">
      <div className="flex items-center gap-2 border-b border-line px-4 py-2.5">
        <div className="flex rounded-md border border-line p-0.5 text-xs">
          {(["task", "goal"] as const).map((k) => (
            <button
              key={k}
              onClick={() => k !== t.kind && (k === "task" && tasks.length > 0 ? confirm(`Turn this goal back into a task? Its ${tasks.length} task(s) stay, but are no longer grouped under it.`) && patch({ kind: k }) : patch({ kind: k, ...(k === "goal" ? { clearParent: true } : {}) }))}
              className={clsx("flex items-center gap-1 rounded px-2 py-0.5 capitalize", t.kind === k ? "bg-white/10 text-fg" : "text-muted")}
            >
              {k === "goal" ? <Target size={11} /> : <Check size={11} />} {k}
            </button>
          ))}
        </div>
        <span className="ml-auto text-[11px] text-faint">Saved automatically</span>
        <button className="btn-ghost p-1" onClick={onClose} title="Close (Esc)">
          <X size={15} />
        </button>
      </div>

      <div className="grid flex-1 content-start gap-4 overflow-y-auto p-4">
        <textarea
          className="w-full resize-none bg-transparent text-base font-semibold outline-none"
          rows={Math.min(4, Math.ceil(title.length / 38) || 1)}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={() => title.trim() && title !== t.title && patch({ title })}
          onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), (e.target as HTMLTextAreaElement).blur())}
        />

        <div className="grid grid-cols-[90px_1fr] items-center gap-x-3 gap-y-2.5 text-sm">
          <span className="text-xs text-muted">Status</span>
          <div className="flex rounded-md border border-line p-0.5 text-xs">
            {statuses.map((s) => (
              <button key={s.id} onClick={() => patch({ status: s.id })} className={clsx("flex-1 rounded px-2 py-1", t.status === s.id ? (s.id === "done" ? "bg-up/20 text-up" : "bg-white/10 text-fg") : "text-muted hover:text-fg")}>
                {s.label}
              </button>
            ))}
          </div>

          <span className="text-xs text-muted">Priority</span>
          <div className="flex gap-1">
            {priorities.map((p) => (
              <button key={p.p} onClick={() => patch({ priority: p.p })} title={p.label} className={clsx("flex-1 rounded border px-1 py-1 text-xs font-semibold", t.priority === p.p ? p.cls : "border-line text-faint hover:text-muted")}>
                {p.short}
              </button>
            ))}
          </div>

          <span className="text-xs text-muted">Due</span>
          <div className="flex items-center gap-2">
            <input type="date" className="input w-auto py-1 text-xs" value={t.dueDate ?? ""} onChange={(e) => patch(e.target.value ? { dueDate: e.target.value } : { clearDue: true })} />
            {t.dueDate && (
              <button className="text-xs text-faint hover:text-fg" onClick={() => patch({ clearDue: true })}>
                Clear
              </button>
            )}
          </div>
          {t.dueDate && (
            <>
              <span />
              <label className="flex items-center gap-2 text-xs text-muted">
                <input type="checkbox" className="accent-accent" checked={t.remind} onChange={(e) => patch({ remind: e.target.checked })} />
                <Bell size={12} /> Pushover reminder on the due date, then daily until done
              </label>
            </>
          )}

          <span className="text-xs text-muted">List</span>
          <select className="input py-1 text-xs" value={t.listId ?? ""} onChange={(e) => patch(e.target.value ? { listId: Number(e.target.value) } : { clearList: true })}>
            <option value="">Inbox</option>
            {lists.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </select>

          {t.kind === "task" && (
            <>
              <span className="text-xs text-muted">Goal</span>
              <select className="input py-1 text-xs" value={t.parentId ?? ""} onChange={(e) => patch(e.target.value ? { parentId: Number(e.target.value) } : { clearParent: true })}>
                <option value="">None</option>
                {goals.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.title}
                  </option>
                ))}
              </select>
            </>
          )}

          <span className="text-xs text-muted">Service</span>
          <select className="input py-1 text-xs" value={t.serviceId ?? ""} onChange={(e) => patch(e.target.value ? { serviceId: Number(e.target.value) } : { clearService: true })}>
            <option value="">None</option>
            {(lp?.services ?? [])
              .slice()
              .sort((a, b) => a.name.localeCompare(b.name))
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
          </select>

          <span className="text-xs text-muted">Host</span>
          <select className="input py-1 text-xs" value={t.hostId ?? ""} onChange={(e) => patch(e.target.value ? { hostId: Number(e.target.value) } : { clearHost: true })}>
            <option value="">None</option>
            {hosts.map((h) => (
              <option key={h.id} value={h.id}>
                {h.name}
              </option>
            ))}
          </select>
        </div>
        {update.error && <div className="text-xs text-down">{update.error.message}</div>}

        {t.kind === "goal" ? <GoalTasks goal={t} tasks={tasks} /> : <Checklist todo={t} />}

        <div>
          <div className="mb-1 flex items-center gap-2">
            <span className="text-[11px] font-semibold tracking-widest text-faint uppercase">Notes</span>
            {t.notes && (
              <button className="ml-auto text-xs text-accent hover:underline" onClick={() => setEditNotes(!editNotes)}>
                {editNotes ? "Preview" : "Edit"}
              </button>
            )}
          </div>
          {editNotes ? (
            <textarea
              className="input min-h-[140px] font-mono text-xs"
              value={notes}
              placeholder="Markdown: steps, links, commands…"
              onChange={(e) => setNotes(e.target.value)}
              onBlur={() => {
                if (notes !== t.notes) patch({ notes });
                if (notes.trim()) setEditNotes(false);
              }}
            />
          ) : (
            <div className="cursor-text rounded-md border border-line px-3 py-2 text-xs" onClick={() => setEditNotes(true)}>
              <MarkdownPreview source={t.notes} />
            </div>
          )}
        </div>

        <div className="flex items-center gap-2 border-t border-line pt-3 text-[11px] text-faint">
          <span>
            Created {fmtDateTime(t.createdAt)}
            {t.completedAt && <> · done {fmtDateTime(t.completedAt)}</>}
          </span>
          <button className="btn-danger ml-auto py-1 text-xs" onClick={() => confirm(`Delete "${t.title}"?${t.kind === "goal" && tasks.length ? " Its tasks are kept." : ""}`) && del.mutate(t.id, { onSuccess: onClose })}>
            <Trash2 size={12} /> Delete
          </button>
        </div>
      </div>
    </aside>
  );
}

function Checklist({ todo }: { todo: Todo }) {
  const add = useAddChecklist();
  const upd = useUpdateChecklist();
  const del = useDeleteChecklist();
  const [text, setText] = useState("");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (text.trim()) add.mutate({ todoId: todo.id, text }, { onSuccess: () => setText("") });
  };
  const done = todo.checklist.filter((c) => c.done).length;
  return (
    <div>
      <div className="mb-1 flex items-center gap-2 text-[11px] font-semibold tracking-widest text-faint uppercase">
        Checklist {todo.checklist.length > 0 && <span className="font-normal normal-case">{done}/{todo.checklist.length}</span>}
      </div>
      <div className="grid gap-0.5">
        {todo.checklist.map((c) => (
          <div key={c.id} className="group flex items-center gap-2 rounded px-1 py-0.5 text-sm hover:bg-white/[0.03]">
            <input type="checkbox" className="accent-accent" checked={c.done} onChange={(e) => upd.mutate({ id: c.id, done: e.target.checked })} />
            <span className={clsx("flex-1", c.done && "text-faint line-through")}>{c.text}</span>
            <button className="text-faint opacity-0 group-hover:opacity-100 hover:text-down" title="Remove" onClick={() => del.mutate(c.id)}>
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
      <form onSubmit={submit} className="mt-1 flex items-center gap-1.5">
        <Plus size={13} className="text-faint" />
        <input className="flex-1 bg-transparent py-1 text-sm outline-none placeholder:text-faint" placeholder="Add a step" value={text} onChange={(e) => setText(e.target.value)} />
      </form>
    </div>
  );
}

function GoalTasks({ goal, tasks }: { goal: Todo; tasks: Todo[] }) {
  const create = useCreateTodo();
  const update = useUpdateTodo();
  const [text, setText] = useState("");
  const pct = goal.tasksTotal ? Math.round((goal.tasksDone / goal.tasksTotal) * 100) : 0;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (text.trim()) create.mutate({ title: text, parentId: goal.id, ...(goal.listId ? { listId: goal.listId } : {}) }, { onSuccess: () => setText("") });
  };
  return (
    <div>
      <div className="mb-1.5 flex items-center gap-2 text-[11px] font-semibold tracking-widest text-faint uppercase">
        Tasks
        {goal.tasksTotal > 0 && (
          <span className="flex items-center gap-1.5 font-normal normal-case">
            <span className="h-1.5 w-24 overflow-hidden rounded-full bg-white/10">
              <span className="block h-full rounded-full bg-up" style={{ width: `${pct}%` }} />
            </span>
            {goal.tasksDone}/{goal.tasksTotal} · {pct}%
          </span>
        )}
      </div>
      <div className="grid gap-0.5">
        {tasks.map((c) => (
          <label key={c.id} className="flex items-center gap-2 rounded px-1 py-0.5 text-sm hover:bg-white/[0.03]">
            <input type="checkbox" className="accent-accent" checked={c.status === "done"} onChange={(e) => update.mutate({ id: c.id, status: e.target.checked ? "done" : "open" })} />
            <span className={clsx("flex-1", c.status === "done" && "text-faint line-through")}>{c.title}</span>
            <span className={clsx("rounded border px-1 text-[10px]", priorities[c.priority - 1].cls)}>{priorities[c.priority - 1].short}</span>
          </label>
        ))}
      </div>
      <form onSubmit={submit} className="mt-1 flex items-center gap-1.5">
        <Plus size={13} className="text-faint" />
        <input className="flex-1 bg-transparent py-1 text-sm outline-none placeholder:text-faint" placeholder="Add a task to this goal" value={text} onChange={(e) => setText(e.target.value)} />
      </form>
    </div>
  );
}
