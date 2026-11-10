import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { Plus } from "lucide-react";
import { useHosts, useLaunchpad } from "@/lib/queries";
import { hostKindIcon, hostKindLabel, runtimeLabel, runtimeTone } from "@/lib/labels";
import type { Host, Service } from "@/lib/types";
import { ServiceIcon } from "@/components/ServiceIcon";
import { StatusDot } from "@/components/StatusDot";
import { useUI } from "@/components/UIProvider";
import { PageHeader } from "../AdminLayout";
import { HostEditor } from "./HostEditor";
import { PveStats, hostResource, usePve } from "@/components/IntegrationBits";
import { WakeButton, useHostLinks } from "@/components/HostActions";

/** Tree of physical hosts → VMs/LXCs → services running on each. */
export function HostsPage() {
  const { data: hosts = [] } = useHosts();
  const { data: lp } = useLaunchpad();
  const ui = useUI();
  const pve = usePve();
  const linksFor = useHostLinks();
  const hostById = useMemo(() => new Map(hosts.map((h) => [h.id, h])), [hosts]);
  const [editor, setEditor] = useState<{ open: boolean; host: Host | null; parentId?: number | null }>({ open: false, host: null });

  const { roots, children, servicesOn, unhosted } = useMemo(() => {
    const ids = new Set(hosts.map((h) => h.id));
    const children = new Map<number, Host[]>();
    const roots: Host[] = [];
    for (const h of hosts) {
      if (h.parentId && ids.has(h.parentId)) children.set(h.parentId, [...(children.get(h.parentId) ?? []), h]);
      else roots.push(h);
    }
    const servicesOn = new Map<number, Service[]>();
    const unhosted: Service[] = [];
    for (const s of lp?.services ?? []) {
      if (s.hosting?.hostId) servicesOn.set(s.hosting.hostId, [...(servicesOn.get(s.hosting.hostId) ?? []), s]);
      else unhosted.push(s);
    }
    const order = (a: Host, b: Host) => (a.vmid ?? 0) - (b.vmid ?? 0) || a.name.localeCompare(b.name);
    roots.sort((a, b) => kindRank(a) - kindRank(b) || a.name.localeCompare(b.name));
    children.forEach((list) => list.sort(order));
    return { roots, children, servicesOn, unhosted };
  }, [hosts, lp?.services]);

  const renderHost = (h: Host, depth: number) => {
    const Icon = hostKindIcon[h.kind];
    const kids = children.get(h.id) ?? [];
    const all = servicesOn.get(h.id) ?? [];
    const isItself = (s: Service) => s.hosting?.runtime === "bare_metal" && !s.hosting.inherited;
    const svcs = all.filter((s) => !isItself(s));
    const itself = all.filter(isItself);
    // Nest services under their parent service when both live here.
    const here = new Set(svcs.map((s) => s.id));
    const svcKids = (pid: number | null) => svcs.filter((s) => (pid == null ? s.parentId == null || !here.has(s.parentId) : s.parentId === pid));
    const renderSvc = (s: Service, d: number, seen: Set<number>): ReactNode =>
      seen.has(s.id) ? null : (
        <div key={s.id}>
          <ServiceLine service={s} depth={d} onClick={() => ui.editService(s)} />
          {svcKids(s.id).map((c) => renderSvc(c, d + 1, new Set([...seen, s.id])))}
        </div>
      );
    return (
      <div key={h.id}>
        <div
          className="group flex cursor-pointer items-center gap-3 border-b border-line/70 py-2 pr-3 hover:bg-white/[0.03]"
          style={{ paddingLeft: 12 + depth * 24 }}
          onClick={() => setEditor({ open: true, host: h })}
        >
          <Icon size={15} className={depth === 0 ? "text-accent" : "text-muted"} />
          <span className="font-medium">{h.name}</span>
          <span className="rounded bg-white/5 px-1.5 py-px text-[10px] text-muted uppercase">{hostKindLabel[h.kind]}</span>
          {h.vmid != null && <span className="font-mono text-[11px] text-faint">#{h.vmid}</span>}
          {h.ip && (
            <Link to={`/network/ips/${h.ip}`} onClick={(e) => e.stopPropagation()} className="font-mono text-xs text-muted hover:text-accent">
              {h.ip}
            </Link>
          )}
          {itself.map((s) => (
            <button
              key={s.id}
              title={`${s.name} runs on this machine itself`}
              onClick={(e) => {
                e.stopPropagation();
                ui.editService(s);
              }}
              className="flex items-center gap-1.5 rounded bg-white/5 px-1.5 py-0.5 text-[11px] hover:bg-white/10"
            >
              <StatusDot status={s.status} className="size-1.5" />
              <ServiceIcon icon={s.icon} name={s.name} className="size-3.5" />
              {s.name}
            </button>
          ))}
          {h.os && <span className="truncate text-xs text-faint">{h.os}</span>}
          <span className="ml-auto flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            {(() => {
              const r = hostResource(pve, h);
              return r ? <PveStats r={r} compact={depth > 0} /> : null;
            })()}
            {linksFor(h, hostById).map((l) => (
              <a key={l.label} href={l.url} target="_blank" rel="noopener noreferrer" className="btn-ghost px-1.5 py-1 text-[11px]" title={l.url}>
                <l.icon size={12} /> {l.label}
              </a>
            ))}
            <WakeButton mac={h.mac} ip={h.ip} compact />
          </span>
          <button
            className="btn-ghost px-2 py-1 text-xs opacity-0 group-hover:opacity-100"
            onClick={(e) => {
              e.stopPropagation();
              setEditor({ open: true, host: null, parentId: h.id });
            }}
          >
            <Plus size={13} /> Add guest
          </button>
        </div>
        {svcKids(null).map((s) => renderSvc(s, depth + 1, new Set()))}
        {kids.map((k) => renderHost(k, depth + 1))}
      </div>
    );
  };

  return (
    <>
      <PageHeader
        title="Hosts"
        subtitle="Physical machines, Proxmox nodes and the VMs/LXCs on them. Services link to a host from their editor."
        actions={
          <button className="btn-primary py-1.5" onClick={() => setEditor({ open: true, host: null, parentId: null })}>
            <Plus size={15} /> Add host
          </button>
        }
      />
      <div className="p-6">
        {hosts.length === 0 ? (
          <div className="rounded-lg border border-dashed border-line-2 p-10 text-center text-sm text-muted">
            Add your Proxmox nodes, NAS and router first, then the VMs and LXCs that run on them.
          </div>
        ) : (
          <div className="overflow-hidden rounded-lg border border-line text-[13px]">{roots.map((h) => renderHost(h, 0))}</div>
        )}
        {unhosted.length > 0 && (
          <div className="mt-6">
            <h2 className="mb-2 text-xs font-semibold tracking-wide text-muted uppercase">Not assigned to a host ({unhosted.length})</h2>
            <div className="flex flex-wrap gap-2">
              {unhosted.map((s) => (
                <button key={s.id} onClick={() => ui.editService(s)} className="flex items-center gap-2 rounded-md border border-line bg-panel px-2.5 py-1.5 text-xs hover:border-line-2">
                  <ServiceIcon icon={s.icon} name={s.name} className="size-4" />
                  {s.name}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
      <HostEditor open={editor.open} host={editor.host} parentId={editor.parentId} onClose={() => setEditor((e) => ({ ...e, open: false }))} />
    </>
  );
}

function ServiceLine({ service: s, depth, onClick }: { service: Service; depth: number; onClick: () => void }) {
  const h = s.hosting!;
  return (
    <div className="flex cursor-pointer items-center gap-2.5 border-b border-line/40 py-1.5 pr-3 text-xs hover:bg-white/[0.03]" style={{ paddingLeft: 12 + depth * 24 }} onClick={onClick}>
      <StatusDot status={s.status} className="size-2" />
      <ServiceIcon icon={s.icon} name={s.name} className="size-4" />
      <span>{s.name}</span>
      {h.inherited ? (
        <span className="text-[10px] text-faint">under {h.viaService}</span>
      ) : (
        <span className={clsx("rounded px-1.5 py-px text-[10px] font-medium", runtimeTone[h.runtime])}>{runtimeLabel[h.runtime]}</span>
      )}
      {!h.inherited && h.vmid != null && <span className="font-mono text-faint">#{h.vmid}</span>}
      {!h.inherited && h.containerName && <span className="font-mono text-faint">{h.containerName}</span>}
      {s.directIp && (
        <span className="ml-auto font-mono text-muted">
          {s.directIp}
          {s.directPort ? `:${s.directPort}` : ""}
        </span>
      )}
    </div>
  );
}

function kindRank(h: Host) {
  return ["proxmox_node", "nas", "router", "server", "vm", "lxc", "device"].indexOf(h.kind);
}
