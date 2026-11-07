import { useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCorners,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { SortableContext, arrayMove, rectSortingStrategy, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { BellOff, Check, ChevronDown, ChevronLeft, ChevronRight, GripVertical, LayoutGrid, Pencil, Plus, Search, Settings2 } from "lucide-react";
import { useLaunchpad, usePatchSettings, useSaveLayout, useSaveSection } from "@/lib/queries";
import type { Section, Service, Settings, TileSize } from "@/lib/types";
import { ServiceIcon } from "@/components/ServiceIcon";
import { StatusDot, statusLabel } from "@/components/StatusDot";
import { ModeToggle } from "@/components/ModeToggle";
import { useUI } from "@/components/UIProvider";

const NONE = "none";
type Containers = Record<string, number[]>; // container key → ordered service ids

const tileDims: Record<TileSize, { min: string; icon: string; text: string; pad: string }> = {
  sm: { min: "104px", icon: "size-8", text: "text-[13px]", pad: "py-3" },
  md: { min: "138px", icon: "size-11", text: "text-[14px]", pad: "py-4" },
  lg: { min: "176px", icon: "size-14", text: "text-[15px]", pad: "py-5" },
};

export function LaunchpadPage() {
  const { data, isLoading } = useLaunchpad();
  const [editing, setEditing] = useState(false);
  const ui = useUI();

  if (isLoading || !data) return <Backdrop settings={undefined} />;
  const { sections, services, settings } = data;

  return (
    <div className="relative min-h-full">
      <Backdrop settings={settings} />
      <div className="relative z-10 mx-auto max-w-[1680px] px-4 pb-16 sm:px-8">
        <Header settings={settings} editing={editing} setEditing={setEditing} />
        {sections.length === 0 && services.length === 0 ? (
          <EmptyState onAddSection={() => ui.editSection(null)} onAddService={() => ui.editService(null)} />
        ) : (
          <Board sections={sections} services={services} editing={editing} glow={settings.tile_glow} />
        )}
      </div>
    </div>
  );
}

// ── background ────────────────────────────────────────────────────────────

function Backdrop({ settings }: { settings: Settings | undefined }) {
  const wp = settings?.wallpaper;
  const style: CSSProperties = wp
    ? { backgroundImage: `url("${wp}")`, filter: settings?.wallpaper_blur ? `blur(${settings.wallpaper_blur}px)` : undefined, transform: settings?.wallpaper_blur ? "scale(1.06)" : undefined }
    : {
        background: `radial-gradient(1200px 700px at 12% -10%, rgba(91,140,255,0.22), transparent 60%),
           radial-gradient(900px 600px at 95% 10%, rgba(167,139,250,0.18), transparent 60%),
           radial-gradient(1000px 800px at 50% 120%, rgba(34,211,238,0.10), transparent 60%),
           linear-gradient(180deg, #0b0e16, #07080c)`,
      };
  return (
    <div className="fixed inset-0 -z-0 overflow-hidden">
      <div className="absolute inset-0 bg-cover bg-center" style={style} />
      {wp && <div className="absolute inset-0 bg-black" style={{ opacity: (settings?.wallpaper_dim ?? 35) / 100 }} />}
      {!wp && <WaveLayer />}
    </div>
  );
}

/** Soft dark dunes, echoing the existing Dashy wallpaper, rendered in SVG. */
function WaveLayer() {
  return (
    <svg className="absolute inset-x-0 bottom-0 h-[70vh] w-full opacity-70" viewBox="0 0 1440 700" preserveAspectRatio="none" aria-hidden>
      <defs>
        <linearGradient id="dune1" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#1b2233" stopOpacity="0.9" />
          <stop offset="1" stopColor="#0a0c12" stopOpacity="1" />
        </linearGradient>
        <linearGradient id="dune2" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#252d42" stopOpacity="0.7" />
          <stop offset="1" stopColor="#0a0c12" stopOpacity="1" />
        </linearGradient>
      </defs>
      <path d="M0 260 C 260 120, 520 420, 820 300 S 1260 120, 1440 220 L1440 700 L0 700Z" fill="url(#dune1)" />
      <path d="M0 470 C 300 330, 640 560, 980 430 S 1330 360, 1440 420 L1440 700 L0 700Z" fill="url(#dune2)" />
    </svg>
  );
}

// ── header ────────────────────────────────────────────────────────────────

function Header({ settings, editing, setEditing }: { settings: Settings; editing: boolean; setEditing: (v: boolean) => void }) {
  const ui = useUI();
  const patch = usePatchSettings();
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
  return (
    <header className="sticky top-0 z-20 -mx-4 mb-6 flex items-center gap-3 px-4 py-4 sm:-mx-8 sm:px-8">
      <div className="flex min-w-0 items-center gap-2.5">
        <img src="/favicon.svg" alt="" className="size-7" />
        <span className="truncate text-lg font-semibold tracking-tight text-white/95">{settings.site_title}</span>
      </div>

      <button
        onClick={ui.openSpotlight}
        className="glass mx-auto flex w-full max-w-md items-center gap-3 rounded-full px-4 py-2 text-left text-sm text-white/50 transition hover:text-white/80 hover:ring-1 hover:ring-white/15"
      >
        <Search size={16} />
        <span className="flex-1">Search services…</span>
        <span className="kbd">{isMac ? "⌘" : "Ctrl"}</span>
        <span className="kbd -ml-2">K</span>
      </button>

      <div className="flex items-center gap-2">
        {settings.alerts_paused && (
          <button
            title="Alerts are paused — click to resume"
            onClick={() => patch.mutate({ alerts_paused: false })}
            className="glass flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-medium text-warn"
          >
            <BellOff size={14} /> Paused
          </button>
        )}
        <ModeToggle />
        <button
          onClick={() => setEditing(!editing)}
          className={clsx("glass flex size-9 items-center justify-center rounded-full transition", editing ? "text-up ring-1 ring-up/50" : "text-white/70 hover:text-white")}
          title={editing ? "Done editing" : "Edit layout"}
        >
          {editing ? <Check size={16} /> : <Pencil size={15} />}
        </button>
        <Link to="/services" className="glass flex size-9 items-center justify-center rounded-full text-white/70 hover:text-white" title="Admin">
          <LayoutGrid size={16} />
        </Link>
      </div>
    </header>
  );
}

function EmptyState({ onAddSection, onAddService }: { onAddSection: () => void; onAddService: () => void }) {
  return (
    <div className="glass mx-auto mt-[12vh] max-w-lg rounded-2xl p-10 text-center">
      <h1 className="text-2xl font-semibold">Welcome to LabDash</h1>
      <p className="mt-2 text-sm text-muted">Start by creating a section (e.g. “Media”), then add your services to it.</p>
      <div className="mt-6 flex justify-center gap-3">
        <button className="btn-primary" onClick={onAddSection}>
          <Plus size={16} /> Add section
        </button>
        <button className="btn-outline" onClick={onAddService}>
          <Plus size={16} /> Add service
        </button>
      </div>
    </div>
  );
}

// ── board with drag & drop ────────────────────────────────────────────────

function buildContainers(sections: Section[], services: Service[]): Containers {
  const c: Containers = { [NONE]: [] };
  for (const s of sections) c[`s${s.id}`] = [];
  for (const sv of services) {
    const key = sv.sectionId && c[`s${sv.sectionId}`] ? `s${sv.sectionId}` : NONE;
    c[key].push(sv.id);
  }
  return c;
}

function Board({ sections, services, editing, glow }: { sections: Section[]; services: Service[]; editing: boolean; glow: boolean }) {
  const ui = useUI();
  const saveLayout = useSaveLayout();
  const saveSection = useSaveSection();
  const byId = useMemo(() => new Map(services.map((s) => [s.id, s])), [services]);
  const [containers, setContainers] = useState<Containers>(() => buildContainers(sections, services));
  const [order, setOrder] = useState<number[]>(() => sections.map((s) => s.id));
  const [activeId, setActiveId] = useState<number | null>(null);

  // Re-sync from server whenever data changes and we're not mid-drag.
  useEffect(() => {
    if (activeId === null) {
      setContainers(buildContainers(sections, services));
      setOrder(sections.map((s) => s.id));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sections, services]);

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  const sectionById = useMemo(() => new Map(sections.map((s) => [s.id, s])), [sections]);

  const findContainer = (id: string | number): string | undefined => {
    if (typeof id === "string" && id in containers) return id;
    const num = typeof id === "number" ? id : Number(String(id).replace(/^t/, ""));
    return Object.keys(containers).find((k) => containers[k].includes(num));
  };

  const persist = (c: Containers, o: number[]) =>
    saveLayout.mutate(o.map((id) => ({ id, serviceIds: c[`s${id}`] ?? [] })));

  const onDragStart = (e: DragStartEvent) => setActiveId(Number(String(e.active.id).slice(1)));

  const onDragOver = (e: DragOverEvent) => {
    const { active, over } = e;
    if (!over) return;
    const from = findContainer(active.id as string);
    const to = findContainer(over.id as string);
    if (!from || !to || from === to || to === NONE) return;
    const id = Number(String(active.id).slice(1));
    setContainers((prev) => {
      const target = prev[to];
      const overIdx = target.indexOf(Number(String(over.id).slice(1)));
      const insertAt = overIdx >= 0 ? overIdx : target.length;
      return {
        ...prev,
        [from]: prev[from].filter((x) => x !== id),
        [to]: [...target.slice(0, insertAt), id, ...target.slice(insertAt)],
      };
    });
  };

  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e;
    setActiveId(null);
    if (!over) return;
    const c = findContainer(active.id as string);
    if (!c) return;
    const items = containers[c];
    const a = items.indexOf(Number(String(active.id).slice(1)));
    const b = items.indexOf(Number(String(over.id).slice(1)));
    const next = a >= 0 && b >= 0 && a !== b ? { ...containers, [c]: arrayMove(items, a, b) } : containers;
    setContainers(next);
    persist(next, order);
  };

  const moveSection = (id: number, dir: -1 | 1) => {
    const i = order.indexOf(id);
    const j = i + dir;
    if (j < 0 || j >= order.length) return;
    const next = arrayMove(order, i, j);
    setOrder(next);
    persist(containers, next);
  };

  const active = activeId !== null ? byId.get(activeId) : undefined;
  const activeSize = active ? (sectionById.get(active.sectionId ?? -1)?.tileSize ?? "md") : "md";

  const renderSection = (key: string, section: Section | null) => {
    const ids = containers[key] ?? [];
    if (!section && ids.length === 0) return null;
    return (
      <SectionBlock
        key={key}
        containerKey={key}
        section={section}
        ids={ids}
        byId={byId}
        editing={editing}
        glow={glow}
        onToggle={() => section && saveSection.mutate({ id: section.id, collapsed: !section.collapsed })}
        onEdit={() => section && ui.editSection(section)}
        onAdd={() => ui.editService(null, section?.id ?? null)}
        onMove={(d) => section && moveSection(section.id, d)}
      />
    );
  };

  return (
    <DndContext sensors={sensors} collisionDetection={closestCorners} onDragStart={onDragStart} onDragOver={onDragOver} onDragEnd={onDragEnd} onDragCancel={() => setActiveId(null)}>
      <div className="grid grid-cols-1 items-start gap-x-10 gap-y-8 md:grid-cols-2 2xl:grid-cols-3">
        {order.map((id) => renderSection(`s${id}`, sectionById.get(id) ?? null))}
        {renderSection(NONE, null)}
        {editing && (
          <button
            onClick={() => ui.editSection(null)}
            className="flex h-32 items-center justify-center gap-2 rounded-2xl border-2 border-dashed border-white/12 text-sm text-white/50 transition hover:border-white/30 hover:text-white/80"
          >
            <Plus size={16} /> Add section
          </button>
        )}
      </div>
      <DragOverlay dropAnimation={{ duration: 160, easing: "ease-out" }}>
        {active ? <TileFace service={active} size={activeSize} glow={glow} lifted /> : null}
      </DragOverlay>
    </DndContext>
  );
}

function SectionBlock({
  containerKey,
  section,
  ids,
  byId,
  editing,
  glow,
  onToggle,
  onEdit,
  onAdd,
  onMove,
}: {
  containerKey: string;
  section: Section | null;
  ids: number[];
  byId: Map<number, Service>;
  editing: boolean;
  glow: boolean;
  onToggle: () => void;
  onEdit: () => void;
  onAdd: () => void;
  onMove: (d: -1 | 1) => void;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: containerKey });
  const collapsed = !!section?.collapsed && !editing;
  const size = section?.tileSize ?? "md";
  const down = ids.filter((id) => byId.get(id)?.status === "down").length;

  return (
    <section>
      <div className="group mb-3 flex items-center gap-2 border-b border-dashed border-white/15 pb-2">
        <button onClick={onToggle} className="flex min-w-0 flex-1 items-center gap-2 text-left" disabled={!section}>
          <ChevronDown size={16} className={clsx("shrink-0 text-white/40 transition-transform", collapsed && "-rotate-90")} />
          {section?.icon && <SectionIcon icon={section.icon} />}
          <h2 className="truncate text-[17px] font-semibold tracking-tight text-white/95">{section?.name ?? "Unsorted"}</h2>
          <span className="text-xs text-white/35 tabular-nums">{ids.length}</span>
          {down > 0 && <span className="rounded-full bg-down/20 px-2 py-0.5 text-[11px] font-semibold text-down">{down} down</span>}
        </button>
        {section && (
          <div className={clsx("flex items-center gap-0.5 transition", editing ? "opacity-100" : "opacity-0 group-hover:opacity-100")}>
            {editing && (
              <>
                <IconBtn title="Move left" onClick={() => onMove(-1)}>
                  <ChevronLeft size={15} />
                </IconBtn>
                <IconBtn title="Move right" onClick={() => onMove(1)}>
                  <ChevronRight size={15} />
                </IconBtn>
              </>
            )}
            <IconBtn title="Add service here" onClick={onAdd}>
              <Plus size={15} />
            </IconBtn>
            <IconBtn title="Section settings" onClick={onEdit}>
              <Settings2 size={15} />
            </IconBtn>
          </div>
        )}
      </div>

      {!collapsed && (
        <SortableContext items={ids.map((i) => `t${i}`)} strategy={rectSortingStrategy}>
          <div
            ref={setNodeRef}
            className={clsx("grid gap-3 rounded-xl transition", editing && "min-h-24 p-1", isOver && editing && "bg-white/5")}
            style={{ gridTemplateColumns: `repeat(auto-fill, minmax(${tileDims[size].min}, 1fr))` }}
          >
            {ids.map((id) => {
              const s = byId.get(id);
              return s ? <SortableTile key={id} service={s} size={size} editing={editing} glow={glow} /> : null;
            })}
            {editing && ids.length === 0 && <div className="col-span-full flex h-20 items-center justify-center text-xs text-white/30">Drop services here</div>}
          </div>
        </SortableContext>
      )}
    </section>
  );
}

function SectionIcon({ icon }: { icon: string }) {
  if (icon.startsWith("emoji:")) return <span className="text-base leading-none">{icon.slice(6)}</span>;
  return <ServiceIcon icon={icon} name="" className="size-5" />;
}

function IconBtn({ children, title, onClick }: { children: ReactNode; title: string; onClick: () => void }) {
  return (
    <button title={title} onClick={onClick} className="rounded-md p-1.5 text-white/50 hover:bg-white/10 hover:text-white">
      {children}
    </button>
  );
}

// ── tiles ─────────────────────────────────────────────────────────────────

function SortableTile({ service, size, editing, glow }: { service: Service; size: TileSize; editing: boolean; glow: boolean }) {
  const ui = useUI();
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: `t${service.id}`, disabled: !editing });
  const style: CSSProperties = { transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.35 : 1 };

  if (editing) {
    return (
      <div ref={setNodeRef} style={style} {...attributes} {...listeners} className="relative cursor-grab touch-none active:cursor-grabbing">
        <TileFace service={service} size={size} glow={glow} />
        <div className="absolute inset-0 rounded-xl ring-1 ring-white/15 ring-inset" />
        <GripVertical size={14} className="absolute top-2 left-1.5 text-white/40" />
        <button
          onPointerDown={(e) => e.stopPropagation()}
          onClick={() => ui.editService(service)}
          className="absolute right-1.5 bottom-1.5 rounded-md bg-black/40 p-1 text-white/70 hover:bg-black/60 hover:text-white"
          title="Edit"
        >
          <Pencil size={12} />
        </button>
      </div>
    );
  }

  const href = service.url || undefined;
  return (
    <a
      ref={setNodeRef}
      href={href}
      target={service.newTab ? "_blank" : undefined}
      rel="noopener noreferrer"
      onClick={(e) => {
        if (!href) {
          e.preventDefault();
          ui.editService(service);
        }
      }}
      onContextMenu={(e) => {
        e.preventDefault();
        ui.editService(service);
      }}
      title={[service.description, service.url || "No URL — click to configure", `Status: ${statusLabel(service.status)}`].filter(Boolean).join("\n")}
      className="block rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-accent"
    >
      <TileFace service={service} size={size} glow={glow} />
    </a>
  );
}

function TileFace({ service, size, glow, lifted }: { service: Service; size: TileSize; glow: boolean; lifted?: boolean }) {
  const d = tileDims[size];
  const down = service.status === "down";
  return (
    <div
      className={clsx(
        "glass group/tile relative flex flex-col items-center gap-2.5 rounded-xl px-3 transition duration-200",
        d.pad,
        lifted ? "scale-105 shadow-2xl ring-1 ring-accent/60" : "hover:-translate-y-0.5 hover:bg-white/[0.06] hover:shadow-[0_14px_30px_-12px_rgba(0,0,0,0.8)]",
        glow && down && "shadow-[0_0_0_1px_rgba(255,77,109,0.5),0_0_24px_-4px_rgba(255,77,109,0.45)]",
      )}
    >
      <StatusDot status={service.status} className="absolute top-2.5 right-2.5" />
      <span className={clsx("w-full truncate px-3 text-center font-medium text-white/90", d.text)}>{service.name}</span>
      <ServiceIcon icon={service.icon} name={service.name} className={clsx(d.icon, "drop-shadow-[0_4px_10px_rgba(0,0,0,0.35)] transition group-hover/tile:scale-105")} />
      {size === "lg" && service.description && <span className="line-clamp-1 w-full text-center text-xs text-white/45">{service.description}</span>}
    </div>
  );
}
