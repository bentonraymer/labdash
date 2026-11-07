import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useNavigate, useParams } from "react-router";
import clsx from "clsx";
import { Download, FileText, Folder as FolderIcon, FolderPlus, Hash, Inbox, Layers, Pencil, Pin, Plus, Search, Trash2 } from "lucide-react";
import { searchNotes, useCreateNote, useDeleteFolder, useFolders, useNotes, useSaveFolder, useTags } from "@/lib/queries";
import type { Folder, NoteHit, NoteSummary } from "@/lib/types";
import { ago } from "@/lib/time";
import { ServiceIcon } from "@/components/ServiceIcon";
import { NoteEditor } from "./NoteEditor";

type Filter = { kind: "all" } | { kind: "pinned" } | { kind: "services" } | { kind: "unfiled" } | { kind: "folder"; id: number } | { kind: "tag"; tag: string };

const FILTER_KEY = "labdash.docs.filter";

export function DocsPage() {
  const { id } = useParams();
  const noteId = id ? Number(id) : null;
  const navigate = useNavigate();
  const { data: notes = [] } = useNotes();
  const { data: folders = [] } = useFolders();
  const { data: tags = [] } = useTags();
  const create = useCreateNote();
  const saveFolder = useSaveFolder();
  const delFolder = useDeleteFolder();
  const [filter, setFilter] = useState<Filter>(() => {
    try {
      return JSON.parse(localStorage.getItem(FILTER_KEY) ?? "") as Filter;
    } catch {
      return { kind: "all" };
    }
  });
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<NoteHit[] | null>(null);

  useEffect(() => {
    try {
      localStorage.setItem(FILTER_KEY, JSON.stringify(filter));
    } catch {
      /* ignore */
    }
  }, [filter]);

  // Full-text search (titles + bodies) once 2+ characters are typed.
  useEffect(() => {
    const term = q.trim();
    if (term.length < 2) {
      setHits(null);
      return;
    }
    const t = setTimeout(() => searchNotes(term).then(setHits).catch(() => setHits([])), 200);
    return () => clearTimeout(t);
  }, [q]);

  const visible = useMemo(() => {
    const byFilter = (n: NoteSummary) => {
      switch (filter.kind) {
        case "pinned":
          return n.pinned;
        case "services":
          return n.serviceId != null;
        case "unfiled":
          return n.folderId == null && n.serviceId == null;
        case "folder":
          return n.folderId === filter.id;
        case "tag":
          return n.tags.some((t) => t.toLowerCase() === filter.tag.toLowerCase());
        default:
          return true;
      }
    };
    return notes.filter(byFilter);
  }, [notes, filter]);

  const newNote = () => {
    const body = {
      folderId: filter.kind === "folder" ? filter.id : null,
      tags: filter.kind === "tag" ? [filter.tag] : [],
      title: "Untitled",
    };
    create.mutate(body, { onSuccess: (n) => navigate(`/docs/${n.id}`) });
  };

  const newFolder = (parentId: number | null = null) => {
    const name = prompt(parentId ? "Subfolder name" : "Folder name");
    if (name?.trim()) saveFolder.mutate({ name: name.trim(), parentId });
  };

  const title = {
    all: "All notes",
    pinned: "Pinned",
    services: "Service docs",
    unfiled: "Unfiled",
    folder: folders.find((f) => filter.kind === "folder" && f.id === filter.id)?.name ?? "Folder",
    tag: filter.kind === "tag" ? `#${filter.tag}` : "",
  }[filter.kind];

  const counts = {
    all: notes.length,
    pinned: notes.filter((n) => n.pinned).length,
    services: notes.filter((n) => n.serviceId != null).length,
    unfiled: notes.filter((n) => n.folderId == null && n.serviceId == null).length,
  };

  return (
    <div className="flex h-full min-h-0">
      {/* Navigation */}
      <aside className="flex w-56 shrink-0 flex-col border-r border-line bg-panel/50">
        <div className="p-3">
          <button className="btn-primary w-full py-1.5" onClick={newNote} disabled={create.isPending}>
            <Plus size={15} /> New note
          </button>
        </div>
        <nav className="flex-1 overflow-y-auto px-2 pb-4 text-[13px]">
          <NavItem icon={<Layers size={14} />} label="All notes" count={counts.all} active={filter.kind === "all"} onClick={() => setFilter({ kind: "all" })} />
          <NavItem icon={<Pin size={14} />} label="Pinned" count={counts.pinned} active={filter.kind === "pinned"} onClick={() => setFilter({ kind: "pinned" })} />
          <NavItem icon={<FileText size={14} />} label="Service docs" count={counts.services} active={filter.kind === "services"} onClick={() => setFilter({ kind: "services" })} />
          <NavItem icon={<Inbox size={14} />} label="Unfiled" count={counts.unfiled} active={filter.kind === "unfiled"} onClick={() => setFilter({ kind: "unfiled" })} />

          <div className="mt-4 flex items-center justify-between px-2 pb-1">
            <span className="text-[10px] font-semibold tracking-widest text-faint uppercase">Folders</span>
            <button className="text-faint hover:text-fg" title="New folder" onClick={() => newFolder()}>
              <FolderPlus size={13} />
            </button>
          </div>
          <FolderTree
            folders={folders}
            parent={null}
            depth={0}
            active={filter.kind === "folder" ? filter.id : null}
            onSelect={(fid) => setFilter({ kind: "folder", id: fid })}
            onNewChild={(fid) => newFolder(fid)}
            onRename={(fo) => {
              const name = prompt("Rename folder", fo.name);
              if (name?.trim()) saveFolder.mutate({ id: fo.id, name: name.trim() });
            }}
            onDelete={(fo) => {
              if (!confirm(`Delete folder “${fo.name}”? Its notes become unfiled; subfolders are removed.`)) return;
              delFolder.mutate(fo.id);
              if (filter.kind === "folder" && filter.id === fo.id) setFilter({ kind: "all" });
            }}
          />
          {folders.length === 0 && <p className="px-2 text-[11px] text-faint">No folders yet.</p>}

          {tags.length > 0 && (
            <>
              <div className="mt-4 px-2 pb-1 text-[10px] font-semibold tracking-widest text-faint uppercase">Tags</div>
              {tags.map((t) => (
                <NavItem
                  key={t.tag}
                  icon={<Hash size={13} />}
                  label={t.tag}
                  count={t.count}
                  active={filter.kind === "tag" && filter.tag.toLowerCase() === t.tag.toLowerCase()}
                  onClick={() => setFilter({ kind: "tag", tag: t.tag })}
                />
              ))}
            </>
          )}
        </nav>
        <div className="border-t border-line p-3">
          <a href="/api/notes/export" download className="btn-ghost w-full justify-start px-2 py-1.5 text-xs" title="Every note as markdown, sorted into folders, with attachments">
            <Download size={14} /> Download all docs (.zip)
          </a>
        </div>
      </aside>

      {/* Note list */}
      <section className="flex w-72 shrink-0 flex-col border-r border-line">
        <div className="border-b border-line p-3">
          <div className="relative">
            <Search size={14} className="absolute top-1/2 left-2.5 -translate-y-1/2 text-faint" />
            <input className="input py-1.5 pl-8" placeholder="Search all notes…" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          {!hits && <div className="mt-2 text-xs font-medium text-muted">{title}</div>}
        </div>
        <div className="flex-1 overflow-y-auto">
          {hits
            ? hits.map((h) => (
                <button key={h.id} onClick={() => navigate(`/docs/${h.id}`)} className={clsx("block w-full border-b border-line/60 px-3 py-2.5 text-left", noteId === h.id ? "bg-accent/12" : "hover:bg-white/[0.03]")}>
                  <div className="truncate text-[13px] font-medium">{h.title}</div>
                  <div className="mt-0.5 line-clamp-2 text-xs text-muted">{highlight(h.snippet)}</div>
                  {h.serviceName && <div className="mt-1 text-[10px] text-faint">{h.serviceName}</div>}
                </button>
              ))
            : visible.map((n) => (
                <button key={n.id} onClick={() => navigate(`/docs/${n.id}`)} className={clsx("block w-full border-b border-line/60 px-3 py-2.5 text-left", noteId === n.id ? "bg-accent/12" : "hover:bg-white/[0.03]")}>
                  <div className="flex items-center gap-1.5">
                    {n.serviceId && <ServiceIcon icon={n.serviceIcon} name={n.serviceName} className="size-3.5 shrink-0" />}
                    <span className="truncate text-[13px] font-medium">{n.title}</span>
                    {n.pinned && <Pin size={11} className="ml-auto shrink-0 text-accent" />}
                  </div>
                  <div className="mt-0.5 line-clamp-2 text-xs text-muted">{n.snippet || <span className="italic text-faint">Empty</span>}</div>
                  <div className="mt-1 flex items-center gap-1.5 text-[10px] text-faint">
                    <span>{ago(n.updatedAt)}</span>
                    {n.tags.slice(0, 3).map((t) => (
                      <span key={t} className="text-accent/80">
                        #{t}
                      </span>
                    ))}
                  </div>
                </button>
              ))}
          {hits?.length === 0 && <p className="p-4 text-center text-xs text-muted">No notes match “{q}”.</p>}
          {!hits && visible.length === 0 && <p className="p-4 text-center text-xs text-muted">No notes here yet.</p>}
        </div>
      </section>

      {/* Editor */}
      <main className="min-w-0 flex-1">
        {noteId ? (
          <NoteEditor key={noteId} noteId={noteId} onDeleted={() => navigate("/docs")} />
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-sm text-muted">
            <FileText size={28} className="text-faint" />
            Select a note or create one.
            <button className="btn-outline py-1.5" onClick={newNote}>
              <Plus size={14} /> New note
            </button>
          </div>
        )}
      </main>
    </div>
  );
}

function NavItem({ icon, label, count, active, onClick }: { icon: ReactNode; label: string; count?: number; active: boolean; onClick: () => void }) {
  return (
    <button onClick={onClick} className={clsx("flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition", active ? "bg-accent/15 text-fg" : "text-muted hover:bg-white/5 hover:text-fg")}>
      <span className="shrink-0">{icon}</span>
      <span className="flex-1 truncate">{label}</span>
      {count != null && <span className="text-[11px] text-faint tabular-nums">{count}</span>}
    </button>
  );
}

function FolderTree({
  folders,
  parent,
  depth,
  active,
  onSelect,
  onNewChild,
  onRename,
  onDelete,
}: {
  folders: Folder[];
  parent: number | null;
  depth: number;
  active: number | null;
  onSelect: (id: number) => void;
  onNewChild: (id: number) => void;
  onRename: (f: Folder) => void;
  onDelete: (f: Folder) => void;
}) {
  const children = folders.filter((f) => f.parentId === parent);
  if (depth > 6) return null;
  return (
    <>
      {children.map((f) => (
        <div key={f.id}>
          <div
            className={clsx("group flex items-center gap-2 rounded-md py-1.5 pr-1 text-left transition", active === f.id ? "bg-accent/15 text-fg" : "text-muted hover:bg-white/5 hover:text-fg")}
            style={{ paddingLeft: 8 + depth * 14 }}
          >
            <button className="flex min-w-0 flex-1 items-center gap-2" onClick={() => onSelect(f.id)}>
              <FolderIcon size={14} className="shrink-0" />
              <span className="truncate">{f.name}</span>
              <span className="text-[11px] text-faint tabular-nums">{f.noteCount}</span>
            </button>
            <span className="hidden items-center gap-0.5 group-hover:flex">
              <button title="New subfolder" className="p-0.5 hover:text-fg" onClick={() => onNewChild(f.id)}>
                <Plus size={11} />
              </button>
              <button title="Rename" className="p-0.5 hover:text-fg" onClick={() => onRename(f)}>
                <Pencil size={11} />
              </button>
              <button title="Delete" className="p-0.5 hover:text-down" onClick={() => onDelete(f)}>
                <Trash2 size={11} />
              </button>
            </span>
          </div>
          <FolderTree folders={folders} parent={f.id} depth={depth + 1} active={active} onSelect={onSelect} onNewChild={onNewChild} onRename={onRename} onDelete={onDelete} />
        </div>
      ))}
    </>
  );
}

/** Render FTS snippets: matches are wrapped in [[ ]] by the server. */
function highlight(snippet: string): ReactNode {
  return snippet.split(/(\[\[.*?\]\])/g).map((part, i) =>
    part.startsWith("[[") ? (
      <mark key={i} className="rounded bg-accent/25 px-0.5 text-fg">
        {part.slice(2, -2)}
      </mark>
    ) : (
      part
    ),
  );
}
