import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useSearchParams } from "react-router";
import clsx from "clsx";
import { DndContext, PointerSensor, KeyboardSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Bell, CalendarClock, Check, ChevronRight, CircleDot, Flag, GripVertical, Inbox, ListTodo, Pencil, Plus, Server, StickyNote, Target, Trash2, X } from "lucide-react";
import { useCreateTodo, useDeleteTodoList, useReorderTodos, useSaveTodoList, useTodos, useUpdateTodo } from "@/lib/queries";
import type { Todo, TodoList } from "@/lib/types";
import { dueInfo, dueTone, priorities, todayISO } from "@/lib/planning";
import { ServiceIcon } from "@/components/ServiceIcon";
import { PageHeader } from "../AdminLayout";
import { TodoDetail } from "./TodoDetail";

type View = { kind: "all" } | { kind: "today" } | { kind: "goals" } | { kind: "inbox" } | { kind: "list"; id: number };
type Sort = "priority" | "due";

const viewKey = (v: View) => (v.kind === "list" ? `list:${v.id}` : v.kind);
const parseView = (s: string | null): View => {
  if (s?.startsWith("list:")) return { kind: "list", id: Number(s.slice(5)) };
  if (s === "today" || s === "goals" || s === "inbox") return { kind: s };
  return { kind: "all" };
};

/** Tasks and goals, grouped into lists, prioritised P1–P4. */
export function TodosPage() {
  const { data, isLoading } = useTodos();
  const [params, setParams] = useSearchParams();
  const view = parseView(params.get("view"));
  const openId = Number(params.get("t")) || null;
  const svcFilter = Number(params.get("service")) || null;
  const hostFilter = Number(params.get("host")) || null;
  const [sort, setSort] = useState<Sort>("priority");
  const [showDone, setShowDone] = useState(false);
  const todos = useMemo(() => data?.todos ?? [], [data]);
  const lists = useMemo(() => data?.lists ?? [], [data]);
  const byId = useMemo(() => new Map(todos.map((t) => [t.id, t])), [todos]);

  const setParam = (k: string, v: string | null) => {
    const next = new URLSearchParams(params);
    if (v === null) next.delete(k);
    else next.set(k, v);
    setParams(next, { replace: true });
  };
  const setView = (v: View) => {
    const next = new URLSearchParams(params);
    next.set("view", viewKey(v));
    next.delete("service");
    next.delete("host");
    setParams(next, { replace: true });
  };

  const today = todayISO();
  const counts = useMemo(() => {
    const open = todos.filter((t) => t.status !== "done");
    return {
      all: open.length,
      today: open.filter((t) => t.dueDate && t.dueDate <= today).length,
      overdue: open.filter((t) => t.dueDate && t.dueDate < today).length,
      goals: open.filter((t) => t.kind === "goal").length,
      inbox: open.filter((t) => t.listId === null).length,
    };
  }, [todos, today]);

  // What this view shows. Tasks inside a visible goal are nested under it
  // (except in the flat Today view and when filtering by service/host).
  const flat = view.kind === "today" || !!svcFilter || !!hostFilter;
  const visible = useMemo(() => {
    let v = todos.filter((t) => showDone || t.status !== "done");
    if (svcFilter) v = v.filter((t) => t.serviceId === svcFilter);
    if (hostFilter) v = v.filter((t) => t.hostId === hostFilter);
    if (view.kind === "today") v = v.filter((t) => t.dueDate && t.dueDate <= today && t.status !== "done");
    if (view.kind === "goals") v = v.filter((t) => t.kind === "goal");
    if (view.kind === "inbox") v = v.filter((t) => t.listId === null);
    if (view.kind === "list") v = v.filter((t) => t.listId === view.id);
    if (!flat) {
      const shown = new Set(v.map((t) => t.id));
      v = v.filter((t) => !(t.parentId && shown.has(t.parentId) && byId.get(t.parentId)?.kind === "goal"));
    }
    return v;
  }, [todos, showDone, svcFilter, hostFilter, view, today, flat, byId]);

  const childrenOf = (goalId: number) =>
    todos.filter((t) => t.parentId === goalId && (showDone || t.status !== "done")).sort((a, b) => a.priority - b.priority || a.position - b.position);

  const groups = useMemo(() => {
    if (sort === "priority") {
      return priorities
        .map((p) => ({ key: `p${p.p}`, label: `${p.short} · ${p.label}`, items: visible.filter((t) => t.priority === p.p).sort((a, b) => Number(a.status === "done") - Number(b.status === "done") || a.position - b.position) }))
        .filter((g) => g.items.length > 0);
    }
    const week = new Date(Date.now() + 7 * 86_400_000).toISOString().slice(0, 10);
    const byDue = [...visible].sort((a, b) => (a.dueDate ?? "9999").localeCompare(b.dueDate ?? "9999") || a.priority - b.priority);
    const buckets: { key: string; label: string; test: (t: Todo) => boolean }[] = [
      { key: "overdue", label: "Overdue", test: (t) => !!t.dueDate && t.dueDate < today && t.status !== "done" },
      { key: "today", label: "Today", test: (t) => t.dueDate === today },
      { key: "week", label: "Next 7 days", test: (t) => !!t.dueDate && t.dueDate > today && t.dueDate <= week },
      { key: "later", label: "Later", test: (t) => !!t.dueDate && t.dueDate > week },
      { key: "none", label: "No due date", test: (t) => !t.dueDate },
      { key: "donepast", label: "Done", test: () => true },
    ];
    const used = new Set<number>();
    return buckets
      .map((b) => {
        const items = byDue.filter((t) => !used.has(t.id) && b.test(t));
        items.forEach((t) => used.add(t.id));
        return { key: b.key, label: b.label, items };
      })
      .filter((g) => g.items.length > 0);
  }, [visible, sort, today]);

  const title =
    view.kind === "today" ? "Today & overdue" : view.kind === "goals" ? "Goals" : view.kind === "inbox" ? "Inbox" : view.kind === "list" ? (lists.find((l) => l.id === view.id)?.name ?? "List") : "All todos";
  const open = openId ? byId.get(openId) : undefined;

  return (
    <>
      <PageHeader title="Todos" subtitle="Tasks and goals for the lab. Due dates can send a Pushover reminder." />
      <div className="flex min-h-0">
        <ListsSidebar view={view} setView={setView} lists={lists} counts={counts} />
        <div className="min-w-0 flex-1 overflow-hidden p-6">
          <div className="mx-auto grid max-w-4xl gap-4">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-base font-semibold">{title}</h2>
              {svcFilter && <FilterChip label={`Service: ${todos.find((t) => t.serviceId === svcFilter)?.serviceName ?? svcFilter}`} onClear={() => setParam("service", null)} />}
              {hostFilter && <FilterChip label={`Host: ${todos.find((t) => t.hostId === hostFilter)?.hostName ?? hostFilter}`} onClear={() => setParam("host", null)} />}
              <div className="ml-auto flex items-center gap-2 text-xs">
                <div className="flex rounded-md border border-line p-0.5">
                  {(["priority", "due"] as Sort[]).map((s) => (
                    <button key={s} onClick={() => setSort(s)} className={clsx("rounded px-2 py-0.5", sort === s ? "bg-white/10 text-fg" : "text-muted hover:text-fg")}>
                      {s === "priority" ? "By priority" : "By due date"}
                    </button>
                  ))}
                </div>
                <label className="flex items-center gap-1.5 text-muted">
                  <input type="checkbox" className="accent-accent" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} /> Show done
                </label>
              </div>
            </div>

            <QuickAdd view={view} />

            {!isLoading && groups.length === 0 && (
              <div className="grid justify-items-center gap-2 rounded-lg border border-dashed border-line-2 px-6 py-10 text-center text-sm text-muted">
                <ListTodo size={26} className="text-faint" />
                {view.kind === "today" ? "Nothing due today. Nice." : view.kind === "goals" ? "No goals yet. Make one with the Goal toggle above." : "Nothing here yet. Add a task above."}
              </div>
            )}

            {groups.map((g) => (
              <Group key={g.key} label={g.label} items={g.items} sortable={sort === "priority"} renderChildren={flat ? undefined : childrenOf} onOpen={(id) => setParam("t", String(id))} showList={view.kind !== "list"} lists={lists} />
            ))}
          </div>
        </div>
        {open && <TodoDetail key={open.id} todo={open} todos={todos} lists={lists} onClose={() => setParam("t", null)} />}
      </div>
    </>
  );
}

function FilterChip({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <span className="flex items-center gap-1 rounded-full border border-accent/40 bg-accent/10 px-2 py-0.5 text-xs">
      {label}
      <button onClick={onClear} className="text-muted hover:text-fg" title="Clear filter">
        <X size={12} />
      </button>
    </span>
  );
}

function ListsSidebar({ view, setView, lists, counts }: { view: View; setView: (v: View) => void; lists: TodoList[]; counts: { all: number; today: number; overdue: number; goals: number; inbox: number } }) {
  const save = useSaveTodoList();
  const del = useDeleteTodoList();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [renaming, setRenaming] = useState<number | null>(null);
  const item = (v: View, icon: ReactNode, label: string, count: number, extra?: ReactNode) => (
    <button
      key={viewKey(v)}
      onClick={() => setView(v)}
      className={clsx("group flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm", viewKey(v) === viewKey(view) ? "bg-accent/15 text-fg" : "text-muted hover:bg-white/5 hover:text-fg")}
    >
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {extra}
      {count > 0 && <span className="text-xs text-faint tabular-nums">{count}</span>}
    </button>
  );
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    save.mutate({ id: renaming ?? undefined, name }, { onSuccess: () => (setAdding(false), setRenaming(null), setName("")) });
  };
  return (
    <aside className="w-56 shrink-0 border-r border-line p-3">
      <div className="grid gap-0.5">
        {item({ kind: "all" }, <ListTodo size={15} />, "All", counts.all)}
        {item({ kind: "today" }, <CalendarClock size={15} />, "Today & overdue", counts.today, counts.overdue > 0 ? <span className="rounded bg-down/20 px-1 text-[10px] text-down">{counts.overdue} late</span> : null)}
        {item({ kind: "goals" }, <Target size={15} />, "Goals", counts.goals)}
        {item({ kind: "inbox" }, <Inbox size={15} />, "Inbox", counts.inbox)}
      </div>
      <div className="mt-4 mb-1 px-2.5 text-[10px] font-semibold tracking-widest text-faint uppercase">Lists</div>
      <div className="grid gap-0.5">
        {lists.map((l) =>
          renaming === l.id ? (
            <form key={l.id} onSubmit={submit}>
              <input className="input py-1 text-sm" autoFocus value={name} onChange={(e) => setName(e.target.value)} onBlur={() => setRenaming(null)} />
            </form>
          ) : (
            <div key={l.id} className="group relative">
              {item({ kind: "list", id: l.id }, <CircleDot size={14} />, l.name, l.open)}
              <div className="absolute top-1 right-1 hidden gap-0.5 bg-panel group-hover:flex">
                <button
                  className="btn-ghost p-1"
                  title="Rename"
                  onClick={() => {
                    setRenaming(l.id);
                    setName(l.name);
                  }}
                >
                  <Pencil size={11} />
                </button>
                <button className="btn-ghost p-1" title="Delete list (its todos move to the Inbox)" onClick={() => confirm(`Delete the list "${l.name}"? Its todos move to the Inbox.`) && del.mutate(l.id)}>
                  <Trash2 size={11} />
                </button>
              </div>
            </div>
          ),
        )}
        {adding ? (
          <form onSubmit={submit}>
            <input className="input py-1 text-sm" autoFocus placeholder="List name" value={name} onChange={(e) => setName(e.target.value)} onBlur={() => !name && setAdding(false)} />
          </form>
        ) : (
          <button className="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm text-faint hover:text-fg" onClick={() => setAdding(true)}>
            <Plus size={14} /> New list
          </button>
        )}
      </div>
    </aside>
  );
}

function QuickAdd({ view }: { view: View }) {
  const create = useCreateTodo();
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<"task" | "goal">(view.kind === "goals" ? "goal" : "task");
  const [priority, setPriority] = useState<1 | 2 | 3 | 4>(3);
  const [due, setDue] = useState("");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!title.trim()) return;
    create.mutate(
      {
        title,
        kind,
        priority,
        ...(view.kind === "list" ? { listId: view.id } : {}),
        ...(due ? { dueDate: due } : view.kind === "today" ? { dueDate: todayISO() } : {}),
      },
      { onSuccess: () => (setTitle(""), setDue("")) },
    );
  };
  return (
    <form onSubmit={submit} className="flex flex-wrap items-center gap-2 rounded-lg border border-line bg-panel p-2">
      <div className="flex rounded-md border border-line p-0.5 text-xs">
        {(["task", "goal"] as const).map((k) => (
          <button type="button" key={k} onClick={() => setKind(k)} className={clsx("flex items-center gap-1 rounded px-2 py-1 capitalize", kind === k ? "bg-white/10 text-fg" : "text-muted")}>
            {k === "goal" ? <Target size={12} /> : <Check size={12} />} {k}
          </button>
        ))}
      </div>
      <input className="input min-w-[200px] flex-1 border-0 bg-transparent" placeholder={kind === "goal" ? "Add a goal, e.g. 10GbE between all nodes" : "Add a task and press Enter"} value={title} onChange={(e) => setTitle(e.target.value)} />
      <select className="input w-auto py-1 text-xs" value={priority} onChange={(e) => setPriority(Number(e.target.value) as 1 | 2 | 3 | 4)} title="Priority">
        {priorities.map((p) => (
          <option key={p.p} value={p.p}>
            {p.short} {p.label}
          </option>
        ))}
      </select>
      <input type="date" className="input w-auto py-1 text-xs" value={due} onChange={(e) => setDue(e.target.value)} title="Due date" />
      <button className="btn-primary py-1.5" disabled={!title.trim() || create.isPending}>
        <Plus size={14} /> Add
      </button>
      {create.error && <span className="w-full text-xs text-down">{create.error.message}</span>}
    </form>
  );
}

function Group({
  label,
  items,
  sortable,
  renderChildren,
  onOpen,
  showList,
  lists,
}: {
  label: string;
  items: Todo[];
  sortable: boolean;
  renderChildren?: (goalId: number) => Todo[];
  onOpen: (id: number) => void;
  showList: boolean;
  lists: TodoList[];
}) {
  const reorder = useReorderTodos();
  const [order, setOrder] = useState<number[] | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }));
  const ids = order && order.length === items.length && order.every((id) => items.some((t) => t.id === id)) ? order : items.map((t) => t.id);
  const sorted = ids.map((id) => items.find((t) => t.id === id)!).filter(Boolean);
  const onDragEnd = (e: DragEndEvent) => {
    if (!e.over || e.active.id === e.over.id) return;
    const next = arrayMove(ids, ids.indexOf(Number(e.active.id)), ids.indexOf(Number(e.over.id)));
    setOrder(next);
    reorder.mutate(next, { onSettled: () => setOrder(null) });
  };
  const body = sorted.map((t) => (
    <div key={t.id}>
      <TodoRow t={t} sortable={sortable} onOpen={onOpen} listName={showList ? lists.find((l) => l.id === t.listId)?.name : undefined} />
      {t.kind === "goal" && renderChildren && <GoalChildren items={renderChildren(t.id)} onOpen={onOpen} />}
    </div>
  ));
  return (
    <section>
      <h3 className="mb-1.5 text-[11px] font-semibold tracking-widest text-faint uppercase">
        {label} <span>· {items.length}</span>
      </h3>
      <div className="overflow-hidden rounded-lg border border-line bg-panel">
        {sortable ? (
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={ids} strategy={verticalListSortingStrategy}>
              {body}
            </SortableContext>
          </DndContext>
        ) : (
          body
        )}
      </div>
    </section>
  );
}

function GoalChildren({ items, onOpen }: { items: Todo[]; onOpen: (id: number) => void }) {
  if (items.length === 0) return null;
  return (
    <div className="border-b border-line/60 bg-black/10 pl-8">
      {items.map((t) => (
        <TodoRow key={t.id} t={t} sortable={false} onOpen={onOpen} nested />
      ))}
    </div>
  );
}

function TodoRow({ t, sortable, onOpen, listName, nested }: { t: Todo; sortable: boolean; onOpen: (id: number) => void; listName?: string; nested?: boolean }) {
  const update = useUpdateTodo();
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: t.id, disabled: !sortable });
  const due = dueInfo(t);
  const prio = priorities[t.priority - 1];
  const checks = t.checklist.length;
  const checksDone = t.checklist.filter((c) => c.done).length;
  const done = t.status === "done";
  const pct = t.tasksTotal ? Math.round((t.tasksDone / t.tasksTotal) * 100) : 0;
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={clsx("group flex items-center gap-2.5 border-b border-line/60 px-3 py-2 text-sm last:border-0 hover:bg-white/[0.03]", isDragging && "relative z-10 bg-panel-2 shadow-lg", done && "opacity-55")}
    >
      {sortable ? (
        <button className="cursor-grab touch-none text-faint opacity-0 group-hover:opacity-100" title="Drag to reorder" {...attributes} {...listeners}>
          <GripVertical size={14} />
        </button>
      ) : (
        !nested && <span className="w-[14px]" />
      )}
      <button
        title={done ? "Mark not done" : "Mark done"}
        onClick={() => update.mutate({ id: t.id, status: done ? "open" : "done" })}
        className={clsx("grid size-[18px] shrink-0 place-items-center border", t.kind === "goal" ? "rounded-md" : "rounded-full", done ? "border-up bg-up/20 text-up" : t.priority === 1 ? "border-red-400" : t.priority === 2 ? "border-orange-400" : "border-line-2 hover:border-muted")}
      >
        {done && <Check size={12} />}
      </button>
      <button className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1 text-left" onClick={() => onOpen(t.id)}>
        {t.kind === "goal" && <Target size={14} className="shrink-0 text-accent" />}
        <span className={clsx("truncate", done && "line-through", t.kind === "goal" && "font-medium")}>{t.title}</span>
        {t.status === "doing" && <span className="rounded bg-accent/20 px-1.5 py-px text-[10px] font-medium text-accent">In progress</span>}
        {t.kind === "goal" && t.tasksTotal > 0 && (
          <span className="flex items-center gap-1.5 text-[11px] text-muted" title={`${t.tasksDone} of ${t.tasksTotal} tasks done`}>
            <span className="h-1.5 w-16 overflow-hidden rounded-full bg-white/10">
              <span className="block h-full rounded-full bg-up" style={{ width: `${pct}%` }} />
            </span>
            {t.tasksDone}/{t.tasksTotal}
          </span>
        )}
      </button>
      <div className="flex shrink-0 items-center gap-2 text-[11px] text-muted">
        {t.notes && <StickyNote size={12} className="text-faint" aria-label="Has notes" />}
        {checks > 0 && (
          <span className={clsx("flex items-center gap-0.5", checksDone === checks && "text-up")} title="Checklist">
            <Check size={11} /> {checksDone}/{checks}
          </span>
        )}
        {t.serviceId && (
          <span className="flex items-center gap-1" title={`Service: ${t.serviceName}`}>
            <ServiceIcon icon={t.serviceIcon} name={t.serviceName} className="size-3.5" />
            <span className="hidden max-w-[110px] truncate lg:inline">{t.serviceName}</span>
          </span>
        )}
        {t.hostId && (
          <span className="flex items-center gap-1" title={`Host: ${t.hostName}`}>
            <Server size={11} />
            <span className="hidden max-w-[100px] truncate lg:inline">{t.hostName}</span>
          </span>
        )}
        {listName && <span className="hidden max-w-[110px] truncate rounded bg-white/5 px-1.5 py-px md:inline">{listName}</span>}
        {due && (
          <span className={clsx("flex items-center gap-1", dueTone[due.tone])}>
            {t.remind && !done && <Bell size={11} aria-label="Pushover reminder on" />}
            {due.text}
          </span>
        )}
        <span className={clsx("rounded border px-1 py-px text-[10px] font-semibold", prio.cls)} title={prio.label}>
          <Flag size={9} className="mr-0.5 inline" />
          {prio.short}
        </span>
        <ChevronRight size={14} className="text-faint opacity-0 group-hover:opacity-100" />
      </div>
    </div>
  );
}

