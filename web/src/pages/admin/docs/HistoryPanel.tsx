import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { RotateCcw, X } from "lucide-react";
import { api } from "@/lib/api";
import { useNoteVersions } from "@/lib/queries";
import { diffLines } from "@/lib/diff";
import type { Note } from "@/lib/types";
import { ago, fmtDateTime } from "@/lib/time";

/** Version list + diff against the current text, with restore. */
export function HistoryPanel({ noteId, currentBody, onClose, onRestored }: { noteId: number; currentBody: string; onClose: () => void; onRestored: (n: Note) => void }) {
  const { data: versions = [], isLoading } = useNoteVersions(noteId, true);
  const [sel, setSel] = useState<number | null>(null);
  const selected = sel ?? versions[0]?.id ?? null;
  const { data: version } = useQuery({
    queryKey: ["note-version", noteId, selected],
    queryFn: () => api.get<{ id: number; title: string; body: string; createdAt: string }>(`/api/notes/${noteId}/versions/${selected}`),
    enabled: selected != null,
  });
  const [restoring, setRestoring] = useState(false);
  const diff = useMemo(() => (version ? diffLines(version.body, currentBody) : []), [version, currentBody]);
  const changes = diff.filter((d) => d.kind !== "same").length;

  const restore = async () => {
    if (!selected || !confirm("Replace the current text with this version? The current text is saved as a version first.")) return;
    setRestoring(true);
    try {
      onRestored(await api.post<Note>(`/api/notes/${noteId}/versions/${selected}/restore`));
    } finally {
      setRestoring(false);
    }
  };

  return (
    <div className="absolute inset-y-0 right-0 z-10 flex w-[min(760px,90%)] border-l border-line-2 bg-panel shadow-2xl">
      <div className="w-52 shrink-0 overflow-y-auto border-r border-line">
        <div className="flex items-center justify-between px-3 py-2.5">
          <span className="text-xs font-semibold">History</span>
          <button className="btn-ghost p-1" onClick={onClose}>
            <X size={14} />
          </button>
        </div>
        {isLoading && <p className="px-3 text-xs text-muted">Loading…</p>}
        {!isLoading && versions.length === 0 && <p className="px-3 text-xs text-muted">No versions yet. A snapshot is saved every few minutes while you edit and when you leave the note.</p>}
        {versions.map((v) => (
          <button
            key={v.id}
            onClick={() => setSel(v.id)}
            className={clsx("block w-full px-3 py-2 text-left text-xs", selected === v.id ? "bg-accent/15" : "hover:bg-white/5")}
            title={fmtDateTime(v.createdAt)}
          >
            <div className="font-medium">{ago(v.createdAt)}</div>
            <div className="text-faint">
              {fmtDateTime(v.createdAt)} · {v.size.toLocaleString()} chars
            </div>
          </button>
        ))}
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        {version ? (
          <>
            <div className="flex items-center gap-3 border-b border-line px-4 py-2.5 text-xs">
              <span className="text-muted">
                Compared with current text: <b className="text-fg">{changes}</b> changed line{changes === 1 ? "" : "s"}
              </span>
              <button className="btn-primary ml-auto py-1 text-xs" disabled={restoring || changes === 0} onClick={restore}>
                <RotateCcw size={13} /> Restore this version
              </button>
            </div>
            <pre className="min-h-0 flex-1 overflow-auto p-3 font-mono text-[12px] leading-relaxed">
              {diff.map((l, i) => (
                <div
                  key={i}
                  className={clsx("px-2 whitespace-pre-wrap", l.kind === "add" && "bg-up/12 text-up", l.kind === "del" && "bg-down/12 text-down line-through decoration-down/40")}
                >
                  <span className="mr-2 inline-block w-3 text-faint select-none">{l.kind === "add" ? "+" : l.kind === "del" ? "−" : " "}</span>
                  {l.text || " "}
                </div>
              ))}
            </pre>
            <p className="border-t border-line px-4 py-1.5 text-[11px] text-faint">
              <span className="text-down">−</span> only in this version · <span className="text-up">+</span> only in the current text
            </p>
          </>
        ) : (
          <div className="p-6 text-xs text-muted">Select a version.</div>
        )}
      </div>
    </div>
  );
}
