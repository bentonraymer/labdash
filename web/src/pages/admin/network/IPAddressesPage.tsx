import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { ArrowRight, Copy, Pencil, Plus, Sparkles } from "lucide-react";
import { useDeleteAssignment, useSaveAssignment, useSubnetMap, useSubnets } from "@/lib/queries";
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
  const [sel, setSel] = useState<string | null>(null);
  const [filter, setFilter] = useState<CellState | null>(null);
  const cells = data?.cells ?? [];
  const counts = useMemo(() => {
    const c: Partial<Record<CellState, number>> = {};
    for (const x of cells) c[x.state] = (c[x.state] ?? 0) + 1;
    return c;
  }, [cells]);
  const selected = cells.find((c) => c.ip === sel) ?? null;
  const used = cells.filter((c) => c.owners.length > 0);

  return (
    <div className="grid gap-5">
      <div className="flex flex-wrap items-center gap-3">
        {subnet.nextFree && (
          <button
            onClick={() => navigator.clipboard?.writeText(subnet.nextFree)}
            title="Copy"
            className="flex items-center gap-2 rounded-md border border-up/40 bg-up/10 px-3 py-1.5 text-sm"
          >
            <Sparkles size={14} className="text-up" />
            Next free static IP <span className="font-mono font-semibold">{subnet.nextFree}</span>
            <Copy size={12} className="text-muted" />
          </button>
        )}
        <span className="text-xs text-muted">
          Gateway <span className="font-mono text-fg">{subnet.gateway || "—"}</span> · DHCP{" "}
          <span className="font-mono text-fg">{subnet.dhcpStart ? `${subnet.dhcpStart} – ${subnet.dhcpEnd}` : "not set"}</span>
        </span>
        <button className="btn-ghost ml-auto py-1 text-xs" onClick={onEdit}>
          <Pencil size={13} /> Edit subnet
        </button>
      </div>

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
            {stateLabel[st]} <span className="tabular-nums text-faint">{counts[st] ?? 0}</span>
          </button>
        ))}
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px]">
        <div className="grid grid-cols-16 gap-1 self-start" style={{ gridTemplateColumns: "repeat(16, minmax(0, 1fr))" }}>
          {cells.map((c) => (
            <button
              key={c.ip}
              onClick={() => setSel(c.ip)}
              title={`${c.ip} · ${stateLabel[c.state]}${c.owners.length ? ` · ${c.owners.map((o) => o.label).join(", ")}` : ""}`}
              className={clsx(
                "flex h-8 items-center justify-center rounded text-[11px] tabular-nums transition",
                cellStyle[c.state],
                sel === c.ip && "ring-2 ring-fg ring-offset-2 ring-offset-bg",
                filter && c.state !== filter && "opacity-15",
              )}
            >
              {lastOctet(c.ip)}
            </button>
          ))}
        </div>
        <aside className="rounded-lg border border-line bg-panel p-4 xl:sticky xl:top-4 xl:self-start">
          {selected ? <CellPanel cell={selected} /> : <p className="text-sm text-muted">Select an address to see who uses it, reserve it, or open its port view.</p>}
        </aside>
      </div>

      <div>
        <h2 className="mb-2 text-xs font-semibold tracking-wide text-muted uppercase">Addresses in use ({used.length})</h2>
        <div className="overflow-hidden rounded-lg border border-line">
          <table className="w-full text-[13px]">
            <tbody>
              {used.map((c) => (
                <tr key={c.ip} className="border-b border-line/70 last:border-0 hover:bg-white/[0.03]">
                  <td className="w-36 px-3 py-2 font-mono">
                    <Link to={`/network/ips/${c.ip}`} className="hover:text-accent">
                      {c.ip}
                    </Link>
                  </td>
                  <td className="w-40 px-3 py-2">
                    <span className="flex items-center gap-2 text-xs">
                      <span className={clsx("size-2.5 rounded-sm", cellStyle[c.state])} />
                      {stateLabel[c.state]}
                    </span>
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex flex-wrap gap-1.5">
                      {c.owners.map((o, i) => (
                        <OwnerChip key={i} owner={o} />
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

function OwnerChip({ owner: o }: { owner: IPCell["owners"][number] }) {
  const prefix = { assignment: o.sub, host: "host", service: o.sub ? `:${o.sub}` : "service", device: "seen" }[o.kind];
  return (
    <span className="flex items-center gap-1.5 rounded bg-white/5 px-1.5 py-0.5 text-xs">
      {o.kind === "service" && <ServiceIcon icon={o.icon ?? ""} name={o.label} className="size-3.5" />}
      {o.label || "—"}
      <span className="font-mono text-[10px] text-faint">{prefix}</span>
    </span>
  );
}

function CellPanel({ cell }: { cell: IPCell }) {
  const save = useSaveAssignment();
  const del = useDeleteAssignment();
  const assignment = cell.owners.find((o) => o.kind === "assignment");
  const [label, setLabel] = useState("");
  useEffect(() => setLabel(assignment?.label ?? ""), [cell.ip, assignment?.label]);

  return (
    <div className="grid gap-3">
      <div>
        <div className="font-mono text-lg font-semibold">{cell.ip}</div>
        <div className="mt-0.5 flex items-center gap-2 text-xs text-muted">
          <span className={clsx("size-2.5 rounded-sm", cellStyle[cell.state])} />
          {stateLabel[cell.state]}
          {cell.inDhcp && cell.state !== "dhcp" && <span className="text-warn">· inside DHCP range</span>}
        </div>
      </div>
      {cell.owners.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {cell.owners.map((o, i) => (
            <OwnerChip key={i} owner={o} />
          ))}
        </div>
      )}
      {!["network", "broadcast", "gateway"].includes(cell.state) && (
        <div className="grid gap-2 border-t border-line pt-3">
          <label className="label mb-0">Reservation label</label>
          <input className="input py-1.5" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="e.g. Future k3s node" />
          <div className="flex flex-wrap gap-2">
            <button className="btn-outline py-1 text-xs" disabled={save.isPending} onClick={() => save.mutate({ ip: cell.ip, kind: "reserved", label })}>
              Reserve
            </button>
            <button className="btn-outline py-1 text-xs" disabled={save.isPending} onClick={() => save.mutate({ ip: cell.ip, kind: "static", label })}>
              Mark static
            </button>
            {assignment && (
              <button className="btn-danger py-1 text-xs" onClick={() => del.mutate(cell.ip)}>
                Release
              </button>
            )}
          </div>
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
