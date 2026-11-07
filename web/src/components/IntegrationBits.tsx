import { useMemo } from "react";
import clsx from "clsx";
import { useHosts, useProxmox } from "@/lib/queries";
import type { Host, NPMServiceStatus, PveResource, Service } from "@/lib/types";

// ── NPM ───────────────────────────────────────────────────────────────────

const npmStyle: Record<NPMServiceStatus["state"], { label: string; cls: string }> = {
  ok: { label: "✓ proxied", cls: "bg-up/12 text-up" },
  mismatch: { label: "≠ mismatch", cls: "bg-warn/15 text-warn" },
  missing: { label: "missing", cls: "bg-down/12 text-down" },
  none: { label: "—", cls: "text-faint" },
};

export function NPMBadge({ status }: { status?: NPMServiceStatus }) {
  if (!status) return <span className="text-faint">…</span>;
  const s = npmStyle[status.state];
  return (
    <span className={clsx("rounded px-1.5 py-0.5 text-[11px] font-medium whitespace-nowrap", s.cls)} title={status.detail || status.forward || ""}>
      {s.label}
    </span>
  );
}

// ── Proxmox stats ─────────────────────────────────────────────────────────

export interface PveIndex {
  byNode: Map<string, PveResource>;
  /** A guest by VMID. Standalone nodes can reuse VMIDs, so pass the node when known. */
  guest: (vmid: number, node?: string) => PveResource | undefined;
  error: string;
  enabled: boolean;
}

export function usePve(): PveIndex | null {
  const { data } = useProxmox();
  return useMemo(() => {
    if (!data?.enabled) return null;
    const byVMID = new Map<number, PveResource[]>();
    const byNode = new Map<string, PveResource>();
    for (const r of data.resources) {
      if (r.type === "node") byNode.set(r.node.toLowerCase(), r);
      else if (r.vmid) byVMID.set(r.vmid, [...(byVMID.get(r.vmid) ?? []), r]);
    }
    const guest = (vmid: number, node?: string) => {
      const list = byVMID.get(vmid);
      if (!list?.length) return undefined;
      if (list.length === 1) return list[0];
      const n = node?.toLowerCase();
      return list.find((r) => r.node.toLowerCase() === n); // ambiguous without a node match
    };
    return { byNode, guest, error: data.error, enabled: true };
  }, [data]);
}

/** The guest a service runs in: its own VMID, or the VM/LXC host it's deployed on. */
export function useServiceGuest() {
  const pve = usePve();
  const { data: hosts = [] } = useHosts();
  const hostById = useMemo(() => new Map(hosts.map((h) => [h.id, h])), [hosts]);
  return (s: Service): PveResource | undefined => {
    if (!pve || !s.hosting) return undefined;
    // The LabDash host tree's root is normally the Proxmox node (pve1…).
    if (s.hosting.vmid) return pve.guest(s.hosting.vmid, s.hosting.nodeName);
    const h = s.hosting.hostId ? hostById.get(s.hosting.hostId) : undefined;
    if (h?.vmid) return pve.guest(h.vmid, h.nodeName);
    // Bare metal on a Proxmox node: the service is the node.
    if (h && s.hosting.runtime === "bare_metal" && h.kind === "proxmox_node") return pve.byNode.get(h.name.toLowerCase());
    return undefined;
  };
}

export function hostResource(pve: PveIndex | null, h: Host): PveResource | undefined {
  if (!pve) return undefined;
  if (h.vmid) return pve.guest(h.vmid, h.nodeName);
  if (h.kind === "proxmox_node") return pve.byNode.get(h.name.toLowerCase());
  return undefined;
}

const pct = (a: number, b: number) => (b > 0 ? Math.min(100, (a / b) * 100) : 0);

function Bar({ value, label, title }: { value: number; label: string; title: string }) {
  // Usage bars: single hue; only the >85% tail turns amber (status), with the % printed.
  return (
    <div className="flex items-center gap-1.5" title={title}>
      <span className="w-7 text-[10px] text-faint">{label}</span>
      <div className="h-1.5 w-14 overflow-hidden rounded-full bg-white/8">
        <div className={clsx("h-full rounded-full", value > 85 ? "bg-warn" : "bg-accent")} style={{ width: `${Math.max(2, value)}%` }} />
      </div>
      <span className="w-8 text-right text-[10px] text-muted tabular-nums">{Math.round(value)}%</span>
    </div>
  );
}

export const gb = (b: number) => (b >= 1e9 ? `${(b / 1e9).toFixed(1)} GB` : `${Math.round(b / 1e6)} MB`);

export function PveStats({ r, compact }: { r: PveResource; compact?: boolean }) {
  const cpu = r.cpu * 100;
  const mem = pct(r.mem, r.maxmem);
  if (r.status !== "running" && r.status !== "online") {
    return <span className="rounded bg-white/5 px-1.5 py-0.5 text-[11px] text-muted">{r.status}</span>;
  }
  return (
    <div className={clsx("flex", compact ? "flex-col gap-0.5" : "items-center gap-3")}>
      <Bar value={cpu} label="CPU" title={`${cpu.toFixed(1)}% of ${r.maxcpu} cores`} />
      <Bar value={mem} label="RAM" title={`${gb(r.mem)} of ${gb(r.maxmem)}`} />
      {!compact && r.maxdisk > 0 && <Bar value={pct(r.disk, r.maxdisk)} label="Disk" title={`${gb(r.disk)} of ${gb(r.maxdisk)}`} />}
    </div>
  );
}

// ── NPM certificate helpers + editor panel ────────────────────────────────

/** Mirrors the backend: exact match or a one-level wildcard. */
export function certCovers(domains: string[], domain: string): boolean {
  const d = domain.toLowerCase();
  return domains.some((c) => {
    c = c.toLowerCase();
    if (c === d) return true;
    if (c.startsWith("*.")) {
      const suffix = c.slice(1);
      return d.endsWith(suffix) && !d.slice(0, -suffix.length).includes(".");
    }
    return false;
  });
}

export function hostOf(url: string): string {
  try {
    return new URL(url).hostname.toLowerCase();
  } catch {
    return "";
  }
}
