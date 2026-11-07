import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { ArrowDown, ArrowUp, BellRing, Check, ChevronRight, EyeOff, Play, Radar, Settings2, Trash2 } from "lucide-react";
import { useAcknowledgeAll, useDeleteDevice, useDeviceEvents, useDevices, useHosts, useRunScan, useScans, useScanTargets, useUpdateDevice } from "@/lib/queries";
import { deviceTypeIcon, osIcon } from "@/lib/deviceIcons";
import type { Device } from "@/lib/types";
import { ago, fmtDateTime } from "@/lib/time";
import { ServiceIcon } from "@/components/ServiceIcon";
import { PageHeader } from "../AdminLayout";
import { WakeButton } from "@/components/HostActions";
import { ScanTargetsEditor } from "./ScanTargetsEditor";

type SortKey = "ip" | "hostname" | "os" | "type" | "vendor" | "ports" | "firstSeen" | "lastSeen";
type Show = "up" | "all" | "new" | "ignored";

/** Display name: your name for it, else the router's, else DNS. */
export const deviceName = (d: Device) => d.customName || d.routerName || d.hostname;

const ipKey = (ip: string) => ip.split(".").map((p) => p.padStart(3, "0")).join(".");

export function DevicesPage() {
  const { data: devices = [], isLoading } = useDevices();
  const { data: targets = [] } = useScanTargets();
  const { data: scans } = useScans();
  const run = useRunScan();
  const ackAll = useAcknowledgeAll();
  const [editor, setEditor] = useState(false);
  const [q, setQ] = useState("");
  const [show, setShow] = useState<Show>("up");
  const [subnet, setSubnet] = useState("all");
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: "ip", dir: 1 });
  const [open, setOpen] = useState<number | null>(null);

  const current = scans?.current;
  const unacked = devices.filter((d) => !d.acknowledged && !d.ignored);
  const lastFailed = scans?.recent.find((r) => r.status === "failed" && !scans.recent.some((x) => x.cidr === r.cidr && x.status === "done" && x.id > r.id));

  const rows = useMemo(() => {
    const term = q.trim().toLowerCase();
    const inSubnet = (ip: string) => {
      if (subnet === "all") return true;
      const [base, bits] = subnet.split("/");
      const toInt = (s: string) => s.split(".").reduce((a, o) => (a << 8) + Number(o), 0) >>> 0;
      const mask = bits === "0" ? 0 : (~0 << (32 - Number(bits))) >>> 0;
      return ((toInt(ip) & mask) >>> 0) === ((toInt(base) & mask) >>> 0);
    };
    const val = (d: Device): string | number => {
      switch (sort.key) {
        case "ip":
          return ipKey(d.ip);
        case "ports":
          return d.ports.filter((p) => p.state === "open").length;
        case "os":
          return d.osName || d.osFamily || "~";
        case "type":
          return d.deviceType || "~";
        case "firstSeen":
          return d.firstSeen;
        case "lastSeen":
          return d.lastSeen;
        case "hostname":
          return (deviceName(d) || "~").toLowerCase();
        default:
          return (d[sort.key] || "~").toLowerCase();
      }
    };
    return devices
      .filter((d) => (show === "ignored" ? d.ignored : !d.ignored))
      .filter((d) => show !== "up" || d.isUp)
      .filter((d) => show !== "new" || !d.acknowledged)
      .filter((d) => inSubnet(d.ip))
      .filter(
        (d) =>
          !term ||
          [d.ip, d.mac, d.hostname, d.customName, d.routerName, d.vendor, d.osName, d.deviceType, d.host?.name ?? "", ...d.services.map((s) => s.name), ...d.ports.map((p) => `${p.port} ${p.product} ${p.title}`)].some((v) =>
            v.toLowerCase().includes(term),
          ),
      )
      .sort((a, b) => {
        const va = val(a);
        const vb = val(b);
        return (va < vb ? -1 : va > vb ? 1 : 0) * sort.dir;
      });
  }, [devices, q, show, subnet, sort]);

  const Th = ({ k, children, className }: { k: SortKey; children: ReactNode; className?: string }) => (
    <th className={clsx("px-3 py-2 text-left font-medium whitespace-nowrap", className)}>
      <button className="inline-flex items-center gap-1 hover:text-fg" onClick={() => setSort((s) => ({ key: k, dir: s.key === k ? (-s.dir as 1 | -1) : 1 }))}>
        {children}
        {sort.key === k && (sort.dir === 1 ? <ArrowUp size={11} /> : <ArrowDown size={11} />)}
      </button>
    </th>
  );

  return (
    <>
      <PageHeader
        title="Devices"
        subtitle={`${devices.filter((d) => d.isUp && !d.ignored).length} up · ${devices.length} known · scanned by nmap from LabDash`}
        actions={
          <button className="btn-outline py-1.5" onClick={() => setEditor(true)}>
            <Settings2 size={15} /> Scan targets
          </button>
        }
      />

      {/* Scan bar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-line px-6 py-3">
        <Radar size={16} className={clsx(current?.running ? "animate-pulse text-accent" : "text-faint")} />
        {targets.length === 0 && (
          <button className="text-sm text-accent hover:underline" onClick={() => setEditor(true)}>
            Add a subnet to scan →
          </button>
        )}
        {targets.map((t) => {
          const running = current?.running && current.targetId === t.id;
          const queued = current?.queued?.includes(t.id);
          return (
            <div key={t.id} className="flex items-center gap-2 rounded-md border border-line bg-panel py-1 pr-1 pl-2.5 text-xs">
              <span className="font-mono">{t.cidr}</span>
              <span className="text-faint">{running ? `${current!.phase} ${Math.round(current!.percent)}%` : queued ? "queued" : `scanned ${ago(t.lastRunAt)}`}</span>
              <button className="btn-ghost p-1" title="Scan now" disabled={running || queued || run.isPending} onClick={() => run.mutate(t.id)}>
                <Play size={12} />
              </button>
            </div>
          );
        })}
        {current?.running && (
          <div className="h-1 w-40 overflow-hidden rounded-full bg-white/10">
            <div className="h-full bg-accent transition-all" style={{ width: `${Math.max(3, current.percent)}%` }} />
          </div>
        )}
        {lastFailed && <span className="text-xs text-down">Last scan of {lastFailed.cidr} failed: {lastFailed.error}</span>}
      </div>

      <div className="grid gap-4 p-6">
        {unacked.length > 0 && (
          <div className="flex items-center gap-3 rounded-md border border-warn/40 bg-warn/10 px-4 py-2.5 text-sm">
            <BellRing size={16} className="text-warn" />
            <span className="flex-1">
              <b>{unacked.length}</b> new device{unacked.length === 1 ? "" : "s"} appeared that {unacked.length === 1 ? "isn't" : "aren't"} linked to a host or service.
            </span>
            <button className="btn-outline py-1 text-xs" onClick={() => setShow("new")}>
              Review
            </button>
            <button className="btn-ghost py-1 text-xs" onClick={() => ackAll.mutate()}>
              <Check size={13} /> Acknowledge all
            </button>
          </div>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <input className="input w-72 py-1.5" placeholder="Search IP, MAC, hostname, vendor, port, title…" value={q} onChange={(e) => setQ(e.target.value)} />
          <div className="flex rounded-md border border-line-2 p-0.5">
            {(
              [
                ["up", "Up"],
                ["all", "All"],
                ["new", `New (${unacked.length})`],
                ["ignored", "Ignored"],
              ] as [Show, string][]
            ).map(([k, l]) => (
              <button key={k} onClick={() => setShow(k)} className={clsx("rounded px-2.5 py-1 text-xs", show === k ? "bg-accent/20 text-fg" : "text-muted hover:text-fg")}>
                {l}
              </button>
            ))}
          </div>
          {targets.length > 1 && (
            <select className="input w-auto py-1.5 text-xs" value={subnet} onChange={(e) => setSubnet(e.target.value)}>
              <option value="all">All subnets</option>
              {targets.map((t) => (
                <option key={t.id} value={t.cidr}>
                  {t.name || t.cidr}
                </option>
              ))}
            </select>
          )}
          <span className="ml-auto text-xs text-faint">{rows.length} shown</span>
        </div>

        <div className="overflow-x-auto rounded-lg border border-line">
          <table className="w-full text-[13px]">
            <thead className="border-b border-line bg-panel text-xs text-muted">
              <tr>
                <th className="w-6" />
                <Th k="ip">Address</Th>
                <th className="px-3 py-2 text-left font-medium">Web</th>
                <Th k="hostname">Name</Th>
                <Th k="os">OS</Th>
                <Th k="type">Type</Th>
                <Th k="vendor">Vendor</Th>
                <Th k="ports">Ports</Th>
                <th className="px-3 py-2 text-left font-medium">Linked</th>
                <Th k="lastSeen">Last seen</Th>
              </tr>
            </thead>
            <tbody>
              {!isLoading && rows.length === 0 && (
                <tr>
                  <td colSpan={10} className="px-3 py-10 text-center text-muted">
                    {devices.length === 0 ? "No devices yet. Add a scan target and run a scan." : "No devices match these filters."}
                  </td>
                </tr>
              )}
              {rows.map((d) => (
                <Fragment key={d.id}>
                  <DeviceRow device={d} open={open === d.id} onToggle={() => setOpen(open === d.id ? null : d.id)} />
                  {open === d.id && (
                    <tr className="border-b border-line bg-panel/60">
                      <td colSpan={10} className="px-6 py-4">
                        <DeviceDetail device={d} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <ScanTargetsEditor open={editor} onClose={() => setEditor(false)} />
    </>
  );
}

function DeviceRow({ device: d, open, onToggle }: { device: Device; open: boolean; onToggle: () => void }) {
  const openPorts = d.ports.filter((p) => p.state === "open");
  const favicons = [...new Map(openPorts.filter((p) => p.favicon).map((p) => [p.favicon, p])).values()].slice(0, 4);
  const os = osIcon(d);
  const TypeIcon = deviceTypeIcon(d.deviceType);
  return (
    <tr onClick={onToggle} className={clsx("cursor-pointer border-b border-line/70 hover:bg-white/[0.03]", open && "bg-white/[0.03]", !d.isUp && "opacity-55")}>
      <td className="pl-3">
        <ChevronRight size={14} className={clsx("text-faint transition-transform", open && "rotate-90")} />
      </td>
      <td className="px-3 py-2">
        <div className="flex items-center gap-2">
          <span className={clsx("size-2 shrink-0 rounded-full", d.isUp ? "bg-up" : "border border-white/30")} title={d.isUp ? "Up" : "Not seen in last scan"} />
          <span className="font-mono">{d.ip}</span>
          {!d.acknowledged && !d.ignored && <span className="rounded bg-warn px-1 py-px text-[9px] font-bold text-bg">NEW</span>}
        </div>
        {d.mac && <div className="pl-4 font-mono text-[10px] text-faint">{d.mac}</div>}
      </td>
      <td className="px-3 py-2">
        <div className="flex items-center gap-1">
          {favicons.map((p) => (
            <img key={p.port} src={p.favicon} alt="" title={`${p.title || p.product} (:${p.port})`} className="size-4 rounded-sm object-contain" />
          ))}
        </div>
      </td>
      <td className="max-w-[220px] px-3 py-2">
        <div className={clsx("truncate", d.customName && "font-medium")}>{deviceName(d) || <span className="text-faint">—</span>}</div>
        {deviceName(d) !== d.hostname && d.hostname && <div className="truncate text-[10px] text-faint">{d.hostname}</div>}
      </td>
      <td className="px-3 py-2">
        <div className="flex max-w-[220px] items-center gap-2">
          {os && <ServiceIcon icon={os} name="" className="size-4 shrink-0" />}
          <span className="truncate" title={d.osName}>
            {d.osName || d.osFamily || <span className="text-faint">—</span>}
          </span>
        </div>
      </td>
      <td className="px-3 py-2 whitespace-nowrap">
        <span className="flex items-center gap-1.5 text-muted">
          <TypeIcon size={13} />
          {d.deviceType}
        </span>
      </td>
      <td className="max-w-[180px] truncate px-3 py-2 text-muted" title={d.vendor}>
        {d.vendor || "—"}
      </td>
      <td className="px-3 py-2">
        <div className="flex max-w-[220px] flex-wrap gap-1">
          {openPorts.slice(0, 5).map((p) => (
            <span key={`${p.port}/${p.proto}`} className="rounded bg-white/5 px-1 font-mono text-[10px] text-muted" title={`${p.service} ${p.product} ${p.version}`}>
              {p.port}
            </span>
          ))}
          {openPorts.length > 5 && <span className="text-[10px] text-faint">+{openPorts.length - 5}</span>}
        </div>
      </td>
      <td className="px-3 py-2">
        <div className="flex flex-wrap items-center gap-1">
          {d.host && <span className="rounded bg-accent/15 px-1.5 py-px text-[11px] text-accent">{d.host.name}</span>}
          {d.services.slice(0, 3).map((s) => (
            <ServiceIcon key={s.id} icon={s.icon ?? ""} name={s.name} className="size-4" />
          ))}
        </div>
      </td>
      <td className="px-3 py-2 text-xs whitespace-nowrap text-muted" title={fmtDateTime(d.lastSeen)}>
        {ago(d.lastSeen)}
      </td>
    </tr>
  );
}

const eventLabel: Record<string, string> = {
  new: "First seen",
  up: "Came back",
  down: "Disappeared",
  ip_change: "IP changed",
  port_open: "Port opened",
  port_closed: "Port closed",
};

function DeviceDetail({ device: d }: { device: Device }) {
  const { data: events = [] } = useDeviceEvents(d.id);
  const { data: hosts = [] } = useHosts();
  const update = useUpdateDevice();
  const del = useDeleteDevice();
  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
      <div className="grid content-start gap-3">
        <div className="overflow-hidden rounded-md border border-line">
          <table className="w-full text-xs">
            <thead className="bg-panel-2 text-muted">
              <tr>
                <th className="px-3 py-1.5 text-left font-medium">Port</th>
                <th className="px-3 py-1.5 text-left font-medium">Service</th>
                <th className="px-3 py-1.5 text-left font-medium">Product</th>
                <th className="px-3 py-1.5 text-left font-medium">Web title</th>
              </tr>
            </thead>
            <tbody>
              {d.ports.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-3 py-4 text-center text-faint">
                    No open ports found.
                  </td>
                </tr>
              )}
              {d.ports.map((p) => (
                <tr key={`${p.port}/${p.proto}`} className={clsx("border-t border-line/60", p.state === "closed" && "text-faint line-through")}>
                  <td className="px-3 py-1.5 font-mono">
                    {p.port}/{p.proto}
                  </td>
                  <td className="px-3 py-1.5">{p.service || "—"}</td>
                  <td className="px-3 py-1.5 text-muted">{`${p.product} ${p.version}`.trim() || "—"}</td>
                  <td className="px-3 py-1.5">
                    <span className="flex items-center gap-1.5">
                      {p.favicon && <img src={p.favicon} alt="" className="size-3.5" />}
                      {p.title || <span className="text-faint">—</span>}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div>
          <div className="mb-1.5 text-[11px] font-medium tracking-wide text-muted uppercase">History</div>
          <div className="grid gap-1 text-xs">
            {events.slice(0, 12).map((e, i) => (
              <div key={i} className="flex gap-3">
                <span className="w-32 shrink-0 text-faint">{fmtDateTime(e.ts)}</span>
                <span>{eventLabel[e.kind] ?? e.kind}</span>
                <span className="font-mono text-muted">{e.detail}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
      <div className="grid content-start gap-3">
        <NameField device={d} />
        <div className="text-xs text-muted">
          First seen <b className="text-fg">{fmtDateTime(d.firstSeen)}</b>
          <br />
          Last seen <b className="text-fg">{fmtDateTime(d.lastSeen)}</b>
        </div>
        <div>
          <label className="label">Linked host</label>
          <select
            className="input py-1.5"
            value={d.host?.id ?? ""}
            onChange={(e) => update.mutate(e.target.value ? { id: d.id, hostId: Number(e.target.value), acknowledged: true } : { id: d.id, clearHost: true })}
          >
            <option value="">— None —</option>
            {hosts.map((h) => (
              <option key={h.id} value={h.id}>
                {h.name}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-wrap gap-2">
          {!d.acknowledged && (
            <button className="btn-outline py-1 text-xs" onClick={() => update.mutate({ id: d.id, acknowledged: true })}>
              <Check size={13} /> Acknowledge
            </button>
          )}
          <button className="btn-outline py-1 text-xs" onClick={() => update.mutate({ id: d.id, ignored: !d.ignored, acknowledged: true })}>
            <EyeOff size={13} /> {d.ignored ? "Unignore" : "Ignore"}
          </button>
          <Link to={`/network/ips/${d.ip}`} className="btn-outline py-1 text-xs">
            Port view
          </Link>
          <WakeButton mac={d.mac} ip={d.ip} />
          <button className="btn-danger py-1 text-xs" onClick={() => confirm("Forget this device? It will reappear (as new) if seen again.") && del.mutate(d.id)}>
            <Trash2 size={13} /> Forget
          </button>
        </div>
      </div>
    </div>
  );
}

/** Custom name, saved per MAC (or per IP for devices without one). */
function NameField({ device: d }: { device: Device }) {
  const update = useUpdateDevice();
  const [v, setV] = useState(d.customName);
  useEffect(() => setV(d.customName), [d.customName]);
  const save = () => v.trim() !== d.customName && update.mutate({ id: d.id, customName: v.trim(), acknowledged: true });
  return (
    <div>
      <label className="label">Name</label>
      <input
        className="input py-1.5"
        value={v}
        onChange={(e) => setV(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
        placeholder={d.routerName || d.hostname || "e.g. Living room TV"}
        maxLength={80}
      />
      <p className="mt-1 text-[11px] text-faint">
        {d.mac ? `Stays with ${d.mac} if its IP changes.` : "No MAC known (different subnet), so the name follows this IP."}
        {d.routerName && ` Router calls it “${d.routerName}”.`}
      </p>
    </div>
  );
}
