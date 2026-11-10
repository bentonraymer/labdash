import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { AlertTriangle, ArrowRight, BookMarked, Boxes, Copy, Info, LayoutGrid, List, Pencil, Plus, Radar, Router as RouterIcon, Sparkles } from "lucide-react";
import { useDeleteAssignment, useRouter, useSaveAssignment, useSubnetMap, useSubnets } from "@/lib/queries";
import { ago } from "@/lib/time";
import { describeIP, type IPFacts } from "./ipFacts";
import type { CellState, IPCell, Subnet } from "@/lib/types";
import { ServiceIcon } from "@/components/ServiceIcon";
import { PageHeader } from "../AdminLayout";
import { SubnetEditor } from "./SubnetEditor";
import { cellStyle, legendOrder, stateLabel } from "./ipStyles";

export function IPAddressesPage() {
  const { data: subnets = [], isLoading } = useSubnets();
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [editor, setEditor] = useState<{ open: boolean; subnet: Subnet | null }>({ open: false, subnet: null });
  const subnet = subnets.find((s) => s.id === selectedId) ?? subnets[0];
  useEffect(() => {
    if (subnet && selectedId === null) setSelectedId(subnet.id);
  }, [subnet, selectedId]);

  return (
    <>
      <PageHeader
        title="IP Addresses"
        subtitle="Static IPs, reservations, DHCP ranges and what's actually on the network."
        actions={
          <button className="btn-primary py-1.5" onClick={() => setEditor({ open: true, subnet: null })}>
            <Plus size={15} /> Add subnet
          </button>
        }
      />
      <div className="grid gap-5 p-6">
        {!isLoading && subnets.length === 0 && (
          <div className="rounded-lg border border-dashed border-line-2 p-10 text-center">
            <p className="text-sm text-muted">Add the subnets you manage (e.g. 192.168.8.0/24 for servers, 192.168.9.0/24 for IoT).</p>
            <button className="btn-primary mt-4" onClick={() => setEditor({ open: true, subnet: null })}>
              <Plus size={15} /> Add subnet
            </button>
          </div>
        )}

        {subnets.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {subnets.map((s) => (
              <button
                key={s.id}
                onClick={() => setSelectedId(s.id)}
                className={clsx(
                  "rounded-lg border px-3.5 py-2 text-left transition",
                  subnet?.id === s.id ? "border-accent bg-accent/10" : "border-line bg-panel hover:border-line-2",
                )}
              >
                <div className="text-sm font-medium">{s.name || s.cidr}</div>
                <div className="font-mono text-[11px] text-muted">
                  {s.cidr}
                  {s.vlan != null ? ` · VLAN ${s.vlan}` : ""} · {s.used}/{s.total} used
                </div>
              </button>
            ))}
          </div>
        )}

        {subnet && <SubnetView subnet={subnet} onEdit={() => setEditor({ open: true, subnet })} />}
      </div>
      <SubnetEditor open={editor.open} subnet={editor.subnet} onClose={() => setEditor((e) => ({ ...e, open: false }))} />
    </>
  );
}

function SubnetView({ subnet, onEdit }: { subnet: Subnet; onEdit: () => void }) {
  const { data } = useSubnetMap(subnet.id);
  const { data: router } = useRouter();
  const routerEnabled = !!router?.enabled;
  const [sel, setSel] = useState<string | null>(null);
  const [filter, setFilter] = useState<CellState | null>(null);
  const [view, setView] = useState<"grid" | "table">(() => {
    try {
      return (localStorage.getItem("labdash.ips.view") as "grid" | "table") || "grid";
    } catch {
      return "grid";
    }
  });
  const setViewPersist = (v: "grid" | "table") => {
    setView(v);
    try {
      localStorage.setItem("labdash.ips.view", v);
    } catch {
      /* ignore */
    }
  };
  const cells = data?.cells ?? [];
  const facts = useMemo(() => new Map(cells.map((c) => [c.ip, describeIP(c, routerEnabled)])), [cells, routerEnabled]);
  const counts = useMemo(() => {
    const c: Partial<Record<CellState, number>> = {};
    for (const x of cells) c[x.state] = (c[x.state] ?? 0) + 1;
    return c;
  }, [cells]);
  const warnCount = useMemo(() => [...facts.values()].filter((f) => f.notes.some((n) => n.level === "warn")).length, [facts]);
  const selected = cells.find((c) => c.ip === sel) ?? null;
  const used = cells.filter((c) => c.owners.length > 0);

  return (
    <div className="grid gap-5">
      <div className="flex flex-wrap items-center gap-3">
        {subnet.nextFree && (
          <button onClick={() => navigator.clipboard?.writeText(subnet.nextFree)} title="Copy" className="flex items-center gap-2 rounded-md border border-up/40 bg-up/10 px-3 py-1.5 text-sm">
            <Sparkles size={14} className="text-up" />
            Next free static IP <span className="font-mono font-semibold">{subnet.nextFree}</span>
            <Copy size={12} className="text-muted" />
          </button>
        )}
        <span className="text-xs text-muted">
          Gateway <span className="font-mono text-fg">{subnet.gateway || "—"}</span> · DHCP{" "}
          <span className="font-mono text-fg">{subnet.dhcpStart ? `${subnet.dhcpStart} – ${subnet.dhcpEnd}` : "not set"}</span>
        </span>
        {warnCount > 0 && (
          <span className="flex items-center gap-1.5 rounded-md bg-warn/15 px-2 py-1 text-xs text-warn">
            <AlertTriangle size={13} /> {warnCount} address{warnCount === 1 ? "" : "es"} need a look
          </span>
        )}
        <div className="ml-auto flex items-center gap-2">
          <div className="flex rounded-md border border-line-2 p-0.5">
            {(["grid", "table"] as const).map((v) => (
              <button key={v} onClick={() => setViewPersist(v)} className={clsx("flex items-center gap-1 rounded px-2.5 py-1 text-xs capitalize", view === v ? "bg-accent/20 text-fg" : "text-muted hover:text-fg")}>
                {v === "grid" ? <LayoutGrid size={12} /> : <List size={12} />} {v}
              </button>
            ))}
          </div>
          <button className="btn-ghost py-1 text-xs" onClick={onEdit}>
            <Pencil size={13} /> Edit subnet
          </button>
        </div>
      </div>

      <div className="grid gap-2">
        <div className="flex flex-wrap gap-1.5">
          {legendOrder.map((st) => (
            <button
              key={st}
              onClick={() => setFilter(filter === st ? null : st)}
              className={clsx(
                "flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs transition",
                filter === st ? "border-accent text-fg" : "border-line text-muted hover:text-fg",
                filter && filter !== st && "opacity-50",
              )}
            >
              <span className={clsx("size-3 rounded-sm", cellStyle[st])} />
              {stateLabel[st]} <span className="text-faint tabular-nums">{counts[st] ?? 0}</span>
            </button>
          ))}
        </div>
        {view === "grid" && (
          <div className="flex flex-wrap items-center gap-4 text-[11px] text-faint">
            <span className="flex items-center gap-1.5">
              <span className="relative inline-block size-4 rounded bg-white/10">
                <span className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-sky-300" />
              </span>
              reserved on the router
            </span>
            <span className="flex items-center gap-1.5">
              <span className="relative inline-block size-4 rounded bg-white/10">
                <span className="absolute inset-x-0.5 bottom-0.5 h-0.5 rounded bg-up" />
              </span>
              online now
            </span>
            <span className="flex items-center gap-1.5">
              <span className="relative inline-block size-4 rounded bg-white/10">
                <span className="absolute top-0 left-0.5 text-[9px] leading-none font-bold text-warn">!</span>
              </span>
              needs a look
            </span>
          </div>
        )}
      </div>

      {view === "grid" ? (
        <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
          <div className="grid gap-1 self-start" style={{ gridTemplateColumns: "repeat(16, minmax(0, 1fr))" }}>
            {cells.map((c) => {
              const f = facts.get(c.ip)!;
              const warn = f.notes.some((n) => n.level === "warn");
              return (
                <button
                  key={c.ip}
                  onClick={() => setSel(c.ip)}
                  title={`${c.ip} · ${stateLabel[c.state]} · ${f.summary}`}
                  className={clsx(
                    "relative flex h-8 items-center justify-center rounded text-[11px] tabular-nums transition",
                    cellStyle[c.state],
                    sel === c.ip && "ring-2 ring-fg ring-offset-2 ring-offset-bg",
                    filter && c.state !== filter && "opacity-15",
                  )}
                >
                  {lastOctet(c.ip)}
                  {f.router && <span className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-sky-300 ring-1 ring-black/40" />}
                  {f.seen?.up && <span className="absolute inset-x-1 bottom-0.5 h-0.5 rounded bg-up" />}
                  {warn && <span className="absolute top-0 left-0.5 text-[10px] leading-none font-bold text-warn drop-shadow">!</span>}
                </button>
              );
            })}
          </div>
          <aside className="rounded-lg border border-line bg-panel p-4 xl:sticky xl:top-4 xl:self-start">
            {selected ? <CellPanel cell={selected} facts={facts.get(selected.ip)!} routerEnabled={routerEnabled} /> : <p className="text-sm text-muted">Select an address to see exactly what's known about it and where each fact comes from.</p>}
          </aside>
        </div>
      ) : (
        <IPTable cells={(filter ? cells.filter((c) => c.state === filter) : used)} facts={facts} routerEnabled={routerEnabled} onSelect={setSel} selected={sel} />
      )}
      {view === "table" && selected && (
        <div className="rounded-lg border border-line bg-panel p-4">
          <CellPanel cell={selected} facts={facts.get(selected.ip)!} routerEnabled={routerEnabled} />
        </div>
      )}
    </div>
  );
}

/** One row per address with each source in its own column. */
function IPTable({ cells, facts, routerEnabled, onSelect, selected }: { cells: IPCell[]; facts: Map<string, IPFacts>; routerEnabled: boolean; onSelect: (ip: string) => void; selected: string | null }) {
  if (!cells.length) return <p className="py-8 text-center text-sm text-muted">Nothing here.</p>;
  return (
    <div className="overflow-x-auto rounded-lg border border-line">
      <table className="w-full text-[13px]">
        <thead className="border-b border-line bg-panel text-xs text-muted">
          <tr>
            <th className="px-3 py-2 text-left font-medium">IP</th>
            <th className="px-3 py-2 text-left font-medium">Running here</th>
            <th className="px-3 py-2 text-left font-medium" title="What you recorded in LabDash">LabDash record</th>
            {routerEnabled && <th className="px-3 py-2 text-left font-medium" title="DHCP reservation on the GL.iNet router">Router reservation</th>}
            <th className="px-3 py-2 text-left font-medium" title="Network scans and the router's client list">On the network</th>
            <th className="px-3 py-2 text-left font-medium">Notes</th>
          </tr>
        </thead>
        <tbody>
          {cells.map((c) => {
            const f = facts.get(c.ip)!;
            return (
              <tr key={c.ip} onClick={() => onSelect(c.ip)} className={clsx("cursor-pointer border-b border-line/70 last:border-0 hover:bg-white/[0.03]", selected === c.ip && "bg-accent/10")}>
                <td className="px-3 py-2 align-top">
                  <div className="flex items-center gap-2 font-mono">
                    <span className={clsx("size-2.5 shrink-0 rounded-sm", cellStyle[c.state])} />
                    <Link to={`/network/ips/${c.ip}`} onClick={(e) => e.stopPropagation()} className="hover:text-accent">
                      {c.ip}
                    </Link>
                  </div>
                  <div className="pl-4.5 text-[10px] text-faint">{stateLabel[c.state]}</div>
                </td>
                <td className="px-3 py-2 align-top">
                  {f.running.length ? (
                    <div className="flex flex-wrap gap-1">
                      {f.running.map((o, i) => (
                        <span key={i} className="flex items-center gap-1 rounded bg-white/5 px-1.5 py-0.5 text-xs">
                          {o.kind === "service" && <ServiceIcon icon={o.icon ?? ""} name={o.label} className="size-3.5" />}
                          {o.label}
                          <span className="text-[10px] text-faint">{o.kind === "service" ? (o.sub ? `:${o.sub}` : "service") : o.sub}</span>
                        </span>
                      ))}
                    </div>
                  ) : (
                    <span className="text-faint">—</span>
                  )}
                </td>
                <td className="px-3 py-2 align-top text-xs">{f.labdash ? <span className="capitalize">{f.labdash.kind}{f.labdash.label ? ` · ${f.labdash.label}` : ""}</span> : <span className="text-faint">—</span>}</td>
                {routerEnabled && (
                  <td className="px-3 py-2 align-top text-xs">
                    {f.router ? (
                      <>
                        <div>{f.router.name}</div>
                        <div className="font-mono text-[10px] text-faint">{f.router.mac}</div>
                      </>
                    ) : (
                      <span className="text-faint">—</span>
                    )}
                  </td>
                )}
                <td className="px-3 py-2 align-top text-xs">
                  {f.seen ? (
                    <>
                      <div className={f.seen.up ? "text-up" : "text-muted"}>{f.seen.up ? "● online" : `○ last seen ${ago(f.seen.lastSeen)}`}</div>
                      <div className="font-mono text-[10px] text-faint">{f.seen.mac || f.seen.label}</div>
                    </>
                  ) : (
                    <span className="text-faint">not seen</span>
                  )}
                </td>
                <td className="max-w-[320px] px-3 py-2 align-top text-xs">
                  {f.notes.map((n, i) => (
                    <div key={i} className={n.level === "warn" ? "text-warn" : "text-faint"}>
                      {n.level === "warn" ? "⚠ " : ""}
                      {n.text}
                    </div>
                  ))}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function Fact({ icon, title, children, empty }: { icon: ReactNode; title: string; children?: ReactNode; empty?: string }) {
  return (
    <div className="flex gap-2.5">
      <span className="mt-0.5 text-muted">{icon}</span>
      <div className="min-w-0 flex-1">
        <div className="text-[11px] font-semibold tracking-wide text-faint uppercase">{title}</div>
        <div className="text-sm">{children ?? <span className="text-faint">{empty}</span>}</div>
      </div>
    </div>
  );
}

function CellPanel({ cell, facts: f, routerEnabled }: { cell: IPCell; facts: IPFacts; routerEnabled: boolean }) {
  const save = useSaveAssignment();
  const del = useDeleteAssignment();
  const [label, setLabel] = useState("");
  useEffect(() => setLabel(f.labdash?.label ?? ""), [cell.ip, f.labdash?.label]);
  const special = ["network", "broadcast", "gateway"].includes(cell.state);

  return (
    <div className="grid gap-4">
      <div>
        <div className="font-mono text-lg font-semibold">{cell.ip}</div>
        <div className="mt-0.5 flex items-center gap-2 text-xs text-muted">
          <span className={clsx("size-2.5 rounded-sm", cellStyle[cell.state])} />
          {stateLabel[cell.state]}
          {cell.inDhcp && <span className="text-faint">· inside the DHCP range</span>}
        </div>
        <p className="mt-1 text-xs text-muted">{f.summary}</p>
      </div>

      {f.notes.length > 0 && (
        <div className="grid gap-1.5">
          {f.notes.map((n, i) => (
            <div key={i} className={clsx("flex gap-2 rounded-md px-2.5 py-1.5 text-xs", n.level === "warn" ? "bg-warn/10 text-warn" : "bg-white/5 text-muted")}>
              {n.level === "warn" ? <AlertTriangle size={13} className="mt-0.5 shrink-0" /> : <Info size={13} className="mt-0.5 shrink-0" />}
              {n.text}
            </div>
          ))}
        </div>
      )}

      <div className="grid gap-3">
        <Fact icon={<Boxes size={14} />} title="Running here (LabDash)" empty="No host or service uses this IP.">
          {f.running.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {f.running.map((o, i) => (
                <span key={i} className="flex items-center gap-1.5 rounded bg-white/5 px-1.5 py-0.5 text-xs">
                  {o.kind === "service" && <ServiceIcon icon={o.icon ?? ""} name={o.label} className="size-3.5" />}
                  {o.label} <span className="text-[10px] text-faint">{o.kind === "service" ? (o.sub ? `port ${o.sub}` : "service") : `${o.sub} host`}</span>
                </span>
              ))}
            </div>
          )}
        </Fact>
        <Fact icon={<BookMarked size={14} />} title="LabDash record (set by you)" empty="None.">
          {f.labdash && (
            <span>
              <b className="capitalize">{f.labdash.kind}</b>
              {f.labdash.label ? ` · ${f.labdash.label}` : ""}
            </span>
          )}
        </Fact>
        {routerEnabled && (
          <Fact icon={<RouterIcon size={14} />} title="Router reservation (from GL.iNet)" empty="None on the router.">
            {f.router && (
              <span>
                {f.router.name} <span className="font-mono text-xs text-muted">{f.router.mac}</span>
              </span>
            )}
          </Fact>
        )}
        <Fact icon={<Radar size={14} />} title="On the network (scans + router)" empty="Never seen by a scan or the router.">
          {f.seen && (
            <span>
              {f.seen.up ? <b className="text-up">Online</b> : <b className="text-muted">Offline</b>}
              <span className="text-xs text-muted"> · last seen {ago(f.seen.lastSeen)}</span>
              <div className="font-mono text-xs text-muted">
                {f.seen.mac} {f.seen.label && f.seen.label !== f.seen.mac ? `· ${f.seen.label}` : ""}
              </div>
            </span>
          )}
        </Fact>
      </div>

      {!special && (
        <div className="grid gap-2 border-t border-line pt-3">
          <label className="label mb-0">LabDash reservation label</label>
          <input className="input py-1.5" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="e.g. Future k3s node" />
          <div className="flex flex-wrap gap-2">
            <button className="btn-outline py-1 text-xs" disabled={save.isPending} onClick={() => save.mutate({ ip: cell.ip, kind: "reserved", label })}>
              Reserve
            </button>
            <button className="btn-outline py-1 text-xs" disabled={save.isPending} onClick={() => save.mutate({ ip: cell.ip, kind: "static", label })}>
              Mark static
            </button>
            {f.labdash && (
              <button className="btn-danger py-1 text-xs" onClick={() => del.mutate(cell.ip)}>
                Release
              </button>
            )}
          </div>
          <p className="text-[11px] text-faint">This only changes LabDash's own record. Router reservations are made from the port view or the Add Service wizard.</p>
          {save.error && <p className="text-xs text-down">{save.error.message}</p>}
        </div>
      )}
      <Link to={`/network/ips/${cell.ip}`} className="btn-ghost justify-start px-0 text-xs text-accent">
        Port view <ArrowRight size={13} />
      </Link>
    </div>
  );
}

const lastOctet = (ip: string) => ip.slice(ip.lastIndexOf(".") + 1);
