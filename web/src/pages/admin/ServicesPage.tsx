import { useMemo, useState, type ReactNode } from "react";
import { ArrowDown, ArrowUp, ExternalLink, Plus } from "lucide-react";
import clsx from "clsx";
import { useLaunchpad } from "@/lib/queries";
import type { Service } from "@/lib/types";
import { ServiceIcon } from "@/components/ServiceIcon";
import { StatusDot, statusLabel } from "@/components/StatusDot";
import { useUI } from "@/components/UIProvider";
import { PageHeader } from "./AdminLayout";

type SortKey = "name" | "section" | "status" | "dnsUrl" | "directIp" | "directPort";

export function ServicesPage() {
  const { data } = useLaunchpad();
  const ui = useUI();
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: "name", dir: 1 });

  const sectionName = useMemo(() => new Map(data?.sections.map((s) => [s.id, s.name]) ?? []), [data?.sections]);

  const rows = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const val = (s: Service): string | number => {
      switch (sort.key) {
        case "section":
          return sectionName.get(s.sectionId ?? -1) ?? "";
        case "directPort":
          return s.directPort ?? 0;
        case "directIp":
          return s.directIp.split(".").map((p) => p.padStart(3, "0")).join(".");
        default:
          return String(s[sort.key]).toLowerCase();
      }
    };
    return (data?.services ?? [])
      .filter((s) => !q || [s.name, s.dnsUrl, s.directIp, String(s.directPort ?? ""), s.description, ...s.tags].some((v) => v.toLowerCase().includes(q)))
      .sort((a, b) => {
        const va = val(a);
        const vb = val(b);
        return (va < vb ? -1 : va > vb ? 1 : 0) * sort.dir;
      });
  }, [data?.services, filter, sort, sectionName]);

  const Th = ({ k, children, className }: { k: SortKey; children: ReactNode; className?: string }) => (
    <th className={clsx("px-3 py-2 text-left font-medium", className)}>
      <button className="inline-flex items-center gap-1 hover:text-fg" onClick={() => setSort((s) => ({ key: k, dir: s.key === k ? (-s.dir as 1 | -1) : 1 }))}>
        {children}
        {sort.key === k && (sort.dir === 1 ? <ArrowUp size={11} /> : <ArrowDown size={11} />)}
      </button>
    </th>
  );

  return (
    <>
      <PageHeader
        title="Services"
        subtitle={`${data?.services.length ?? 0} services · hosting details (node, VM/LXC/Docker) arrive in build phase 4`}
        actions={
          <>
            <input className="input w-64 py-1.5" placeholder="Filter…" value={filter} onChange={(e) => setFilter(e.target.value)} />
            <button className="btn-primary py-1.5" onClick={() => ui.editService(null)}>
              <Plus size={15} /> Add service
            </button>
          </>
        }
      />
      <div className="overflow-x-auto">
        <table className="w-full text-[13px]">
          <thead className="sticky top-0 z-10 border-b border-line bg-panel text-xs text-muted">
            <tr>
              <Th k="status" className="w-10">
                Up
              </Th>
              <Th k="name">Name</Th>
              <Th k="section">Section</Th>
              <Th k="dnsUrl">DNS URL</Th>
              <Th k="directIp">IP</Th>
              <Th k="directPort">Port</Th>
              <th className="px-3 py-2 text-left font-medium">Tags</th>
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
                    <span className="font-medium">{s.name}</span>
                  </div>
                </td>
                <td className="px-3 py-2 text-muted">{sectionName.get(s.sectionId ?? -1) ?? "—"}</td>
                <td className="px-3 py-2 font-mono text-xs text-muted">{s.dnsUrl || "—"}</td>
                <td className="px-3 py-2 font-mono text-xs">{s.directIp || "—"}</td>
                <td className="px-3 py-2 font-mono text-xs">{s.directPort ?? "—"}</td>
                <td className="px-3 py-2">
                  <div className="flex flex-wrap gap-1">
                    {s.tags.map((t) => (
                      <span key={t} className="rounded bg-white/5 px-1.5 py-0.5 text-[11px] text-muted">
                        {t}
                      </span>
                    ))}
                  </div>
                </td>
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
                <td colSpan={8} className="px-3 py-10 text-center text-muted">
                  No services yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
