import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { AlertTriangle, Check, CloudOff, Columns2, Eye, History, Loader2, Pencil, Pin, PinOff, Trash2, X } from "lucide-react";
import { keys, useDeleteNote, useFolders, useLaunchpad, useNote } from "@/lib/queries";
import type { Note } from "@/lib/types";
import { ago } from "@/lib/time";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { MarkdownPreview } from "@/components/MarkdownPreview";
import { ServiceIcon } from "@/components/ServiceIcon";
import { HistoryPanel } from "./HistoryPanel";

type Mode = "edit" | "split" | "preview";
type SaveState = "saved" | "dirty" | "saving" | "error" | "conflict";

const AUTOSAVE_MS = 800;
const MODE_KEY = "labdash.docs.mode";

interface Fields {
  title: string;
  body: string;
  tags: string[];
  folderId: number | null;
  serviceId: number | null;
  pinned: boolean;
}

/** PATCH a note; keepalive lets the request finish while the tab closes. */
async function patchNote(id: number, payload: Record<string, unknown>, keepalive = false) {
  const res = await fetch(`/api/notes/${id}`, {
    method: "PATCH",
    credentials: "same-origin",
    keepalive,
    headers: { "Content-Type": "application/json", "X-LabDash": "1" },
    body: JSON.stringify(payload),
  });
  const data = await res.json().catch(() => ({}));
  return { status: res.status, data };
}

function snapshot(id: number, keepalive = false) {
  return fetch(`/api/notes/${id}/snapshot`, { method: "POST", credentials: "same-origin", keepalive, headers: { "X-LabDash": "1" } }).catch(() => {});
}

function toPayload(p: Partial<Fields>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (p.title !== undefined) out.title = p.title;
  if (p.body !== undefined) out.body = p.body;
  if (p.tags !== undefined) out.tags = p.tags;
  if (p.pinned !== undefined) out.pinned = p.pinned;
  if ("folderId" in p) {
    if (p.folderId == null) out.clearFolder = true;
    else out.folderId = p.folderId;
  }
  if ("serviceId" in p) {
    if (p.serviceId == null) out.clearService = true;
    else out.serviceId = p.serviceId;
  }
  return out;
}

export function NoteEditor({ noteId, onDeleted }: { noteId: number; onDeleted: () => void }) {
  const { data: note, error } = useNote(noteId);
  if (error) return <div className="p-8 text-sm text-down">{(error as Error).message}</div>;
  if (!note) return <div className="p-8 text-sm text-muted">Loading…</div>;
  return <Editor key={note.id} note={note} onDeleted={onDeleted} />;
}

function Editor({ note, onDeleted }: { note: Note; onDeleted: () => void }) {
  const qc = useQueryClient();
  const { data: lp } = useLaunchpad();
  const { data: folders = [] } = useFolders();
  const del = useDeleteNote();
  const [f, setF] = useState<Fields>({ title: note.title, body: note.body, tags: note.tags, folderId: note.folderId, serviceId: note.serviceId, pinned: note.pinned });
  const [mode, setMode] = useState<Mode>(() => {
    try {
      return (localStorage.getItem(MODE_KEY) as Mode) || "split";
    } catch {
      return "split";
    }
  });
  const [save, setSave] = useState<SaveState>("saved");
  const [savedAt, setSavedAt] = useState(note.updatedAt);
  const [conflict, setConflict] = useState<Note | null>(null);
  const [history, setHistory] = useState(false);
  const [editorKey, setEditorKey] = useState(0);

  const pending = useRef<Partial<Fields>>({});
  const base = useRef(note.updatedAt);
  const inflight = useRef(false);
  const timer = useRef<number | undefined>(undefined);
  const latest = useRef(f);
  latest.current = f;

  const flush = useCallback(
    async (opts: { keepalive?: boolean; force?: boolean } = {}) => {
      window.clearTimeout(timer.current);
      if (inflight.current && !opts.keepalive) {
        timer.current = window.setTimeout(() => flush(), 200);
        return;
      }
      const patch = pending.current;
      if (!Object.keys(patch).length && !opts.force) return;
      pending.current = {};
      inflight.current = true;
      setSave("saving");
      try {
        const payload = opts.force ? { ...toPayload(latest.current), force: true } : { ...toPayload(patch), baseUpdatedAt: base.current };
        const { status, data } = await patchNote(note.id, payload, opts.keepalive);
        if (status === 200) {
          base.current = data.updatedAt;
          setSavedAt(data.updatedAt);
          setConflict(null);
          if (Object.keys(pending.current).length) {
            setSave("dirty");
            timer.current = window.setTimeout(() => flush(), AUTOSAVE_MS);
          } else {
            setSave("saved");
          }
          qc.invalidateQueries({ queryKey: keys.notes });
        } else if (status === 409) {
          pending.current = { ...patch, ...pending.current };
          setConflict(data.note as Note);
          setSave("conflict");
        } else {
          throw new Error(data.error ?? `HTTP ${status}`);
        }
      } catch {
        pending.current = { ...patch, ...pending.current };
        setSave("error");
        timer.current = window.setTimeout(() => flush(), 5000);
      } finally {
        inflight.current = false;
      }
    },
    [note.id, qc],
  );

  const change = useCallback(
    (patch: Partial<Fields>, immediate = false) => {
      setF((p) => ({ ...p, ...patch }));
      Object.assign(pending.current, patch);
      setSave((s) => (s === "conflict" ? s : "dirty"));
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => flush(), immediate ? 0 : AUTOSAVE_MS);
    },
    [flush],
  );

  // Save on tab hide/close and when leaving the note.
  useEffect(() => {
    const onHide = () => {
      if (document.visibilityState === "hidden" || document.visibilityState === undefined) {
        if (Object.keys(pending.current).length) flush({ keepalive: true });
        snapshot(note.id, true);
      }
    };
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (Object.keys(pending.current).length) {
        flush({ keepalive: true });
        e.preventDefault();
      }
    };
    document.addEventListener("visibilitychange", onHide);
    window.addEventListener("pagehide", onHide);
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      document.removeEventListener("visibilitychange", onHide);
      window.removeEventListener("pagehide", onHide);
      window.removeEventListener("beforeunload", onBeforeUnload);
      window.clearTimeout(timer.current);
      if (Object.keys(pending.current).length) flush({ keepalive: true }).then(() => snapshot(note.id, true));
      else snapshot(note.id, true);
    };
  }, [flush, note.id]);

  const setModePersist = (m: Mode) => {
    setMode(m);
    try {
      localStorage.setItem(MODE_KEY, m);
    } catch {
      /* private mode */
    }
  };

  const loadTheirs = () => {
    if (!conflict) return;
    pending.current = {};
    base.current = conflict.updatedAt;
    setF({ title: conflict.title, body: conflict.body, tags: conflict.tags, folderId: conflict.folderId, serviceId: conflict.serviceId, pinned: conflict.pinned });
    setEditorKey((k) => k + 1);
    setConflict(null);
    setSave("saved");
  };

  const restored = (n: Note) => {
    pending.current = {};
    base.current = n.updatedAt;
    setF((p) => ({ ...p, title: n.title, body: n.body }));
    setEditorKey((k) => k + 1);
    setSave("saved");
    setHistory(false);
    qc.invalidateQueries({ queryKey: keys.notes });
  };

  const service = lp?.services.find((s) => s.id === f.serviceId);
  const words = f.body.trim() ? f.body.trim().split(/\s+/).length : 0;

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Title + toolbar */}
      <div className="border-b border-line px-6 pt-4 pb-3">
        <div className="flex items-start gap-3">
          <input
            className="min-w-0 flex-1 bg-transparent text-xl font-semibold tracking-tight outline-none placeholder:text-faint"
            value={f.title}
            onChange={(e) => change({ title: e.target.value })}
            placeholder="Untitled"
          />
          <SaveBadge state={save} savedAt={savedAt} />
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-2 text-xs">
          <select className="input w-auto py-1 text-xs" value={f.folderId ?? ""} onChange={(e) => change({ folderId: e.target.value ? Number(e.target.value) : null }, true)}>
            <option value="">No folder</option>
            {folders.map((fo) => (
              <option key={fo.id} value={fo.id}>
                📁 {fo.name}
              </option>
            ))}
          </select>
          <select className="input w-auto max-w-48 py-1 text-xs" value={f.serviceId ?? ""} onChange={(e) => change({ serviceId: e.target.value ? Number(e.target.value) : null }, true)}>
            <option value="">Not linked to a service</option>
            {lp?.services
              .slice()
              .sort((a, b) => a.name.localeCompare(b.name))
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
          </select>
          {service && <ServiceIcon icon={service.icon} name={service.name} className="size-4" />}
          <TagInput tags={f.tags} onChange={(tags) => change({ tags }, true)} />
          <div className="ml-auto flex items-center gap-1">
            <button className="btn-ghost p-1.5" title={f.pinned ? "Unpin" : "Pin"} onClick={() => change({ pinned: !f.pinned }, true)}>
              {f.pinned ? <PinOff size={14} className="text-accent" /> : <Pin size={14} />}
            </button>
            <div className="flex rounded-md border border-line-2 p-0.5">
              {(
                [
                  ["edit", Pencil, "Editor"],
                  ["split", Columns2, "Editor + preview"],
                  ["preview", Eye, "Preview"],
                ] as const
              ).map(([m, Icon, label]) => (
                <button key={m} title={label} onClick={() => setModePersist(m)} className={clsx("rounded p-1", mode === m ? "bg-accent/20 text-fg" : "text-muted hover:text-fg")}>
                  <Icon size={14} />
                </button>
              ))}
            </div>
            <button className={clsx("btn-ghost p-1.5", history && "text-accent")} title="Version history" onClick={() => setHistory(!history)}>
              <History size={14} />
            </button>
            <button
              className="btn-ghost p-1.5 hover:text-down"
              title="Delete note"
              onClick={() => {
                if (!confirm(`Delete “${f.title}” and its history?`)) return;
                pending.current = {};
                del.mutate(note.id, { onSuccess: onDeleted });
              }}
            >
              <Trash2 size={14} />
            </button>
          </div>
        </div>
      </div>

      {conflict && (
        <div className="flex items-center gap-3 border-b border-warn/40 bg-warn/10 px-6 py-2 text-sm">
          <AlertTriangle size={15} className="text-warn" />
          <span className="flex-1">This note was changed somewhere else (another tab or device) {ago(conflict.updatedAt)}.</span>
          <button className="btn-outline py-1 text-xs" onClick={loadTheirs}>
            Load their version
          </button>
          <button className="btn-primary py-1 text-xs" onClick={() => flush({ force: true })}>
            Keep mine
          </button>
        </div>
      )}

      <div className="relative flex min-h-0 flex-1">
        {mode !== "preview" && (
          <div className={clsx("min-h-0 min-w-0", mode === "split" ? "w-1/2 border-r border-line" : "flex-1")}>
            <MarkdownEditor
              key={editorKey}
              noteId={note.id}
              value={f.body}
              onChange={(body) => change({ body })}
              onBlur={() => flush().then(() => snapshot(note.id))}
              onSave={() => flush()}
              placeholder="Write in markdown… paste or drop images and files to attach them."
            />
          </div>
        )}
        {mode !== "edit" && (
          <div className={clsx("min-h-0 overflow-y-auto px-8 py-5", mode === "split" ? "w-1/2" : "mx-auto w-full max-w-3xl")}>
            <MarkdownPreview source={f.body} />
          </div>
        )}
        {history && <HistoryPanel noteId={note.id} currentBody={f.body} onClose={() => setHistory(false)} onRestored={restored} />}
      </div>
      <div className="flex items-center gap-4 border-t border-line px-6 py-1.5 text-[11px] text-faint">
        <span>{words} words</span>
        <span>⌘S saves now · changes save automatically</span>
      </div>
    </div>
  );
}

function SaveBadge({ state, savedAt }: { state: SaveState; savedAt: string }) {
  const map: Record<SaveState, { icon: ReactNode; text: string; cls: string }> = {
    saved: { icon: <Check size={12} />, text: `Saved ${ago(savedAt)}`, cls: "text-faint" },
    dirty: { icon: <Pencil size={12} />, text: "Editing…", cls: "text-muted" },
    saving: { icon: <Loader2 size={12} className="animate-spin" />, text: "Saving…", cls: "text-muted" },
    error: { icon: <CloudOff size={12} />, text: "Offline — retrying", cls: "text-warn" },
    conflict: { icon: <AlertTriangle size={12} />, text: "Conflict", cls: "text-warn" },
  };
  const m = map[state];
  return (
    <span className={clsx("mt-1.5 flex shrink-0 items-center gap-1.5 text-xs", m.cls)} aria-live="polite">
      {m.icon}
      {m.text}
    </span>
  );
}

function TagInput({ tags, onChange }: { tags: string[]; onChange: (t: string[]) => void }) {
  const [v, setV] = useState("");
  const add = () => {
    const t = v.trim().replace(/^#/, "");
    if (t && !tags.some((x) => x.toLowerCase() === t.toLowerCase())) onChange([...tags, t]);
    setV("");
  };
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      add();
    } else if (e.key === "Backspace" && !v && tags.length) {
      onChange(tags.slice(0, -1));
    }
  };
  return (
    <div className="flex flex-wrap items-center gap-1 rounded-md border border-line-2 px-1.5 py-0.5">
      {tags.map((t) => (
        <span key={t} className="flex items-center gap-0.5 rounded bg-accent/15 px-1.5 py-0.5 text-[11px] text-accent">
          #{t}
          <button onClick={() => onChange(tags.filter((x) => x !== t))} aria-label={`Remove ${t}`}>
            <X size={10} />
          </button>
        </span>
      ))}
      <input className="w-20 bg-transparent py-0.5 text-xs outline-none placeholder:text-faint" value={v} onChange={(e) => setV(e.target.value)} onKeyDown={onKey} onBlur={add} placeholder="+ tag" />
    </div>
  );
}
