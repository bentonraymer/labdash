import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { Archive, ArchiveRestore, ArrowDown, ArrowUp, Download, ExternalLink, Plus, Trash2 } from "lucide-react";
import { useDeleteService, useLaunchpad, useNPMStatus, useSaveService } from "@/lib/queries";
import { ago, fmtDateTime } from "@/lib/time";
import { NPMBadge, PveStats, useServiceGuest } from "@/components/IntegrationBits";
import { runtimeLabel, runtimeTone } from "@/lib/labels";
import type { Runtime, Service } from "@/lib/types";
import { ServiceIcon } from "@/components/ServiceIcon";
import { StatusDot, statusLabel } from "@/components/StatusDot";
import { useUI } from "@/components/UIProvider";
import { PageHeader } from "./AdminLayout";

type SortKey = "name" | "section" | "status" | "node" | "host" | "runtime" | "ref" | "ip" | "port" | "dns";

const ipKey = (ip: string) => ip.split(".").map((p) => p.padStart(3, "0")).join(".");

export function ServicesPage() {
  const { data } = useLaunchpad();
  const ui = useUI();
  const { data: npm } = useNPMStatus();
  const guestOf = useServiceGuest();
  const showNPM = !!npm?.enabled;
  const [filter, setFilter] = useState("");
  const [view, setView] = useState<"active" | "archived">("active");
  const archived = data?.archivedServices ?? [];
  const [runtime, setRuntime] = useState<Runtime | "all" | "none">("all");
  const [node, setNode] = useState("all");
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: "name", dir: 1 });

  const sectionName = useMemo(() => new Map(data?.sections.map((s) => [s.id, s.name]) ?? []), [data?.sections]);
  const services = data?.services ?? [];
  const nodes = useMemo(() => [...new Set(services.map((s) => s.hosting?.nodeName).filter(Boolean) as string[])].sort(), [services]);

  const val = (s: Service, k: SortKey): string | number => {
    switch (k) {
      case "section":
        return sectionName.get(s.sectionId ?? -1) ?? "~";
      case "status":
        return s.status;
      case "node":
        return s.hosting?.nodeName || "~";
      case "host":
        return s.hosting?.hostName || "~";
      case "runtime":
        return s.hosting?.runtime ?? "~";
      case "ref":
        return s.hosting?.vmid ?? (s.hosting?.containerName || "~");
      case "ip":
        return s.directIp ? ipKey(s.directIp) : "~";
      case "port":
        return s.directPort ?? 99999;
      case "dns":
        return s.dnsUrl || "~";
      default:
        return s.name.toLowerCase();
    }
  };

  const rows = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return services
      .filter((s) => runtime === "all" || (runtime === "none" ? !s.hosting : s.hosting?.runtime === runtime))
      .filter((s) => node === "all" || s.hosting?.nodeName === node)
      .filter(
        (s) =>
          !q ||
          [s.name, s.dnsUrl, s.directIp, String(s.directPort ?? ""), s.description, s.hosting?.hostName ?? "", s.hosting?.containerName ?? "", String(s.hosting?.vmid ?? ""), ...s.tags].some(
            (v) => v.toLowerCase().includes(q),
          ),
      )
      .sort((a, b) => {
        const va = val(a, sort.key);
        const vb = val(b, sort.key);
        return (va < vb ? -1 : va > vb ? 1 : 0) * sort.dir || a.name.localeCompare(b.name);
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [services, filter, runtime, node, sort, sectionName]);

  const exportCSV = () => {
    const head = ["Name", "Section", "Status", "Node", "Host", "Type", "VMID", "Container", "IP", "Port", "DNS URL", "Direct URL", "Tags"];
    const esc = (v: unknown) => `"${String(v ?? "").replace(/"/g, '""')}"`;
    const lines = rows.map((s) =>
      [
        s.name,
        sectionName.get(s.sectionId ?? -1) ?? "",
        s.status,
        s.hosting?.nodeName,
        s.hosting?.hostName,
        s.hosting ? runtimeLabel[s.hosting.runtime] : "",
        s.hosting?.vmid,
        s.hosting?.containerName,
        s.directIp,
        s.directPort,
        s.dnsUrl,
        s.directUrl,
        s.tags.join(" "),
      ]
        .map(esc)
        .join(","),
    );
    const blob = new Blob([[head.join(","), ...lines].join("\n")], { type: "text/csv" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `labdash-services-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  const Th = ({ k, children, className }: { k: SortKey; children: ReactNode; className?: string }) => (
    <th className={clsx("px-3 py-2 text-left font-medium whitespace-nowrap", className)}>
      <button className="inline-flex items-center gap-1 hover:text-fg" onClick={() => setSort((s) => ({ key: k, dir: s.key === k ? (-s.dir as 1 | -1) : 1 }))}>
        {children}
        {sort.key === k && (sort.dir === 1 ? <ArrowUp size={11} /> : <ArrowDown size={11} />)}
      </button>
    </th>
  );

  const runtimes: (Runtime | "all" | "none")[] = ["all", "docker", "lxc", "vm", "bare_metal", "other", "none"];

  return (
    <>
      <PageHeader
        title="Services"
        subtitle={`${rows.length} of ${services.length} services`}
        actions={
          <>
            <button className="btn-ghost py-1.5" onClick={exportCSV} title="Download the filtered table as CSV">
              <Download size={15} /> CSV
            </button>
            <button className="btn-primary py-1.5" onClick={() => ui.editService(null)}>
              <Plus size={15} /> Add service
            </button>
          </>
        }
      />
      <div className="flex gap-1 border-b border-line px-6">
        {(
          [
            ["active", `Active (${services.length})`],
            ["archived", `Archived (${archived.length})`],
          ] as const
        ).map(([v, label]) => (
          <button
            key={v}
            onClick={() => setView(v)}
            className={clsx("-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm transition", view === v ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg")}
          >
            {v === "archived" && <Archive size={13} />}
            {label}
          </button>
        ))}
      </div>
      {view === "archived" ? (
        <ArchivedTable services={archived} sectionName={sectionName} />
      ) : (
      <>
      <div className="flex flex-wrap items-center gap-2 border-b border-line px-6 py-3">
        <input className="input w-64 py-1.5" placeholder="Filter by name, IP, host, tag…" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <div className="flex rounded-md border border-line-2 p-0.5">
          {runtimes.map((r) => (
            <button
              key={r}
              onClick={() => setRuntime(r)}
              className={clsx("rounded px-2.5 py-1 text-xs", runtime === r ? "bg-accent/20 text-fg" : "text-muted hover:text-fg")}
            >
              {r === "all" ? "All" : r === "none" ? "Unassigned" : runtimeLabel[r]}
            </button>
          ))}
        </div>
        {nodes.length > 0 && (
          <select className="input w-auto py-1.5 text-xs" value={node} onChange={(e) => setNode(e.target.value)}>
            <option value="all">All nodes</option>
            {nodes.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        )}
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-[13px]">
          <thead className="sticky top-0 z-10 border-b border-line bg-panel text-xs text-muted">
            <tr>
              <Th k="status" className="w-10">
                Up
              </Th>
              <Th k="name">Name</Th>
              <Th k="node">Node</Th>
              <Th k="host">Host</Th>
              <Th k="runtime">Type</Th>
              <Th k="ref">VMID / container</Th>
              <Th k="ip">IP</Th>
              <Th k="port">Port</Th>
              <Th k="dns">DNS URL</Th>
              {showNPM && <th className="px-3 py-2 text-left font-medium">NPM</th>}
              <th className="px-3 py-2 text-left font-medium">Load</th>
              <Th k="section">Section</Th>
              <th className="w-10" />
            </tr>
          </thead>
          <tbody>
            {rows.map((s) => (
              <tr key={s.id} onClick={() => ui.editService(s)} className="cursor-pointer border-b border-line/70 hover:bg-white/[0.03]">
                <td className="px-3 py-2" title={statusLabel(s.status)}>
                  <StatusDot status={s.status} />
                </td>
                <td className="px-3 py-2">
                  <div className="flex items-center gap-2.5">
                    <ServiceIcon icon={s.icon} name={s.name} className="size-5" />
                    <span className="font-medium whitespace-nowrap">{s.name}</span>
                  </div>
                </td>
                <td className="px-3 py-2 whitespace-nowrap text-muted">{s.hosting?.nodeName || "—"}</td>
                <td className="px-3 py-2 whitespace-nowrap">{s.hosting?.hostName && (s.hosting.hostName !== s.hosting.nodeName || s.hosting.runtime === "bare_metal") ? s.hosting.hostName : <span className="text-faint">—</span>}</td>
                <td className="px-3 py-2">
                  {s.hosting ? (
                    <span className={clsx("rounded px-1.5 py-0.5 text-[11px] font-medium", runtimeTone[s.hosting.runtime])}>{runtimeLabel[s.hosting.runtime]}</span>
                  ) : (
                    <span className="text-faint">—</span>
                  )}
                </td>
                <td className="px-3 py-2 font-mono text-xs text-muted">{s.hosting?.vmid ?? (s.hosting?.containerName || "—")}</td>
                <td className="px-3 py-2 font-mono text-xs" onClick={(e) => e.stopPropagation()}>
                  {s.directIp ? (
                    <Link to={`/network/ips/${s.directIp}`} className="hover:text-accent">
                      {s.directIp}
                    </Link>
                  ) : (
                    "—"
                  )}
                </td>
                <td className="px-3 py-2 font-mono text-xs">{s.directPort ?? "—"}</td>
                <td className="max-w-[260px] truncate px-3 py-2 font-mono text-xs text-muted">{s.dnsUrl || "—"}</td>
                {showNPM && (
                  <td className="px-3 py-2">
                    {npm?.error ? <span className="text-[11px] text-down" title={npm.error}>error</span> : <NPMBadge status={npm?.services?.[s.id]} />}
                  </td>
                )}
                <td className="px-3 py-2">{(() => { const g = guestOf(s); return g ? <PveStats r={g} compact /> : <span className="text-faint">—</span>; })()}</td>
                <td className="px-3 py-2 whitespace-nowrap text-muted">{sectionName.get(s.sectionId ?? -1) ?? <span className="text-faint italic">hidden</span>}</td>
                <td className="px-3 py-2">
                  {s.url && (
                    <a href={s.url} target="_blank" rel="noopener noreferrer" onClick={(e) => e.stopPropagation()} className="text-faint hover:text-accent" title={s.url}>
                      <ExternalLink size={14} />
                    </a>
                  )}
                </td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={13} className="px-3 py-10 text-center text-muted">
                  No services match.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      </>
      )}
    </>
  );
}

/** Archived services: kept for their docs and history, out of everything else. */
function ArchivedTable({ services, sectionName }: { services: Service[]; sectionName: Map<number, string> }) {
  const ui = useUI();
  const save = useSaveService();
  const del = useDeleteService();
  if (services.length === 0) {
    return <p className="px-6 py-10 text-center text-sm text-muted">No archived services. Archive one from its editor when you retire it; its docs and history are kept.</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-[13px]">
        <thead className="border-b border-line bg-panel text-xs text-muted">
          <tr>
            <th className="px-3 py-2 text-left font-medium">Name</th>
            <th className="px-3 py-2 text-left font-medium">Archived</th>
            <th className="px-3 py-2 text-left font-medium">Was on</th>
            <th className="px-3 py-2 text-left font-medium">IP</th>
            <th className="px-3 py-2 text-left font-medium">Section</th>
            <th className="w-48" />
          </tr>
        </thead>
        <tbody>
          {services
            .slice()
            .sort((a, b) => (b.archivedAt ?? "").localeCompare(a.archivedAt ?? ""))
            .map((s) => (
              <tr key={s.id} onClick={() => ui.editService(s)} className="cursor-pointer border-b border-line/70 opacity-80 hover:bg-white/[0.03] hover:opacity-100">
                <td className="px-3 py-2">
                  <div className="flex items-center gap-2.5">
                    <ServiceIcon icon={s.icon} name={s.name} className="size-5 grayscale" />
                    <span className="font-medium">{s.name}</span>
                  </div>
                </td>
                <td className="px-3 py-2 text-xs text-muted" title={s.archivedAt ? fmtDateTime(s.archivedAt) : ""}>
                  {ago(s.archivedAt)}
                </td>
                <td className="px-3 py-2 text-xs text-muted">{s.hosting ? `${runtimeLabel[s.hosting.runtime]}${s.hosting.hostName ? ` · ${s.hosting.hostName}` : ""}` : "—"}</td>
                <td className="px-3 py-2 font-mono text-xs text-muted">{s.directIp ? `${s.directIp}${s.directPort ? `:${s.directPort}` : ""}` : "—"}</td>
                <td className="px-3 py-2 text-xs text-muted">{sectionName.get(s.sectionId ?? -1) ?? "—"}</td>
                <td className="px-3 py-2" onClick={(e) => e.stopPropagation()}>
                  <div className="flex justify-end gap-1">
                    <button className="btn-outline py-1 text-xs" onClick={() => save.mutate({ id: s.id, archived: false })} disabled={save.isPending}>
                      <ArchiveRestore size={13} /> Restore
                    </button>
                    <button
                      className="btn-ghost p-1.5 hover:text-down"
                      title="Delete permanently"
                      onClick={() => confirm(`Delete “${s.name}” permanently? Its monitor history goes too; its docs page is kept as an unlinked note.`) && del.mutate(s.id)}
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
        </tbody>
      </table>
    </div>
  );
}

