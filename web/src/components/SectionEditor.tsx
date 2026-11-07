import { useEffect, useState, type FormEvent } from "react";
import { Trash2 } from "lucide-react";
import { Modal } from "./Modal";
import { useDeleteSection, useSaveSection } from "@/lib/queries";
import type { Section, TileSize } from "@/lib/types";

export function SectionEditor({ open, onClose, section }: { open: boolean; onClose: () => void; section: Section | null }) {
  const save = useSaveSection();
  const del = useDeleteSection();
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("");
  const [tileSize, setTileSize] = useState<TileSize>("md");

  useEffect(() => {
    if (!open) return;
    setName(section?.name ?? "");
    setIcon(section?.icon ?? "");
    setTileSize(section?.tileSize ?? "md");
  }, [open, section]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ id: section?.id, name, icon, tileSize }, { onSuccess: onClose });
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={section ? "Edit section" : "Add section"}
      footer={
        <>
          {section && (
            <button
              type="button"
              className="btn-danger mr-auto"
              onClick={() => confirm(`Delete section “${section.name}”? Its services will move to “Unsorted”.`) && del.mutate(section.id, { onSuccess: onClose })}
            >
              <Trash2 size={15} /> Delete
            </button>
          )}
          <button type="button" className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" form="section-form" className="btn-primary" disabled={!name.trim() || save.isPending}>
            Save
          </button>
        </>
      }
    >
      <form id="section-form" onSubmit={submit} className="grid gap-4">
        <div className="grid grid-cols-[1fr_140px] gap-3">
          <div>
            <label className="label">Name</label>
            <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Media" />
          </div>
          <div>
            <label className="label">Icon (emoji)</label>
            <input className="input" value={icon.replace(/^emoji:/, "")} onChange={(e) => setIcon(e.target.value ? `emoji:${e.target.value}` : "")} placeholder="🎬" />
          </div>
        </div>
        <div>
          <label className="label">Tile size</label>
          <div className="flex gap-2">
            {(["sm", "md", "lg"] as const).map((s) => (
              <button
                type="button"
                key={s}
                onClick={() => setTileSize(s)}
                className={tileSize === s ? "btn border border-accent bg-accent/15 text-fg" : "btn-outline"}
              >
                {{ sm: "Compact", md: "Medium", lg: "Large" }[s]}
              </button>
            ))}
          </div>
        </div>
      </form>
    </Modal>
  );
}
