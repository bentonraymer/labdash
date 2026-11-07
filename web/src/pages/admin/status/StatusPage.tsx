import { Fragment, useMemo, useState } from "react";
import clsx from "clsx";
import { Activity, Bell, BellOff, CalendarClock, ChevronRight, Globe, Network, Pencil, Plus, RefreshCw, Wrench, Zap, type LucideIcon } from "lucide-react";
import { useCheckMonitor, useIncidents, useLaunchpad, useMaintenance, useMonitorHistory, useMonitors, usePatchSettings, useSaveMonitor, type HistoryRange } from "@/lib/queries";
import type { Incident, Maintenance, Monitor, Status } from "@/lib/types";
import { ago, duration, fmtDateTime, pct } from "@/lib/time";
import { ServiceIcon } from "@/components/ServiceIcon";
import { StatusDot, statusLabel } from "@/components/StatusDot";
import { UptimeBars } from "@/components/UptimeBars";
import { LatencyChart } from "@/components/LatencyChart";
import { PageHeader } from "../AdminLayout";
import { MonitorEditor } from "./MonitorEditor";
import { MaintenanceEditor } from "./MaintenanceEditor";

type Tab = "monitors" | "incidents" | "maintenance";

const kindIcon: Record<Monitor["kind"], LucideIcon> = { http: Globe, tcp: Network, ping: Zap, dns: Activity };

/** Service icon for service monitors; a type glyph for standalone ones. */
function MonitorIcon({ serviceId, icon, name, kind, className }: { serviceId: number | null; icon: string; name: string; kind?: Monitor["kind"]; className?: string }) {
  if (serviceId) return <ServiceIcon icon={icon} name={name} className={className} />;
  const Icon = kind ? kindIcon[kind] : Activity;
  return (
    <span className={clsx("flex items-center justify-center rounded-md bg-white/5 text-muted", className)}>
      <Icon size={13} />
    </span>
  );
}

const statusOrder: Record<Status, number> = { down: 0, pending: 1, maintenance: 2, up: 3, paused: 4, unknown: 5 };
const statusText: Record<Status, string> = { up: "text-up", down: "text-down", pending: "text-warn", maintenance: "text-maint", paused: "text-faint", unknown: "text-faint" };

export function StatusPage() {
  const { data: monitors = [], isLoading } = useMonitors();
  const { data: maint = [] } = useMaintenance();
  const { data: lp } = useLaunchpad();
  const patch = usePatchSettings();
  const [tab, setTab] = useState<Tab>("monitors");
  const [editor, setEditor] = useState<{ open: boolean; monitor: Monitor | null }>({ open: false, monitor: null });
  const [mEditor, setMEditor] = useState<{ open: boolean; win: Maintenance | null }>({ open: false, win: null });
  const [expanded, setExpanded] = useState<number | null>(null);

  const sorted = useMemo(
    () => [...monitors].sort((a, b) => statusOrder[a.status] - statusOrder[b.status] || (a.name || a.serviceName).localeCompare(b.name || b.serviceName)),
    [monitors],
  );
  const counts = useMemo(() => {
    const c: Record<string, number> = {};
    for (const m of monitors) c[m.status] = (c[m.status] ?? 0) + 1;
    return c;
  }, [monitors]);
  const avg24 = useMemo(() => {
    const v = monitors.filter((m) => m.enabled && m.uptime24h != null).map((m) => m.uptime24h as number);
    return v.length ? v.reduce((a, b) => a + b, 0) / v.length : null;
  }, [monitors]);
  const activeMaint = maint.filter((w) => w.active);
  const paused = lp?.settings.alerts_paused;

  return (
    <>
      <PageHeader
        title="Status"
        subtitle={`${monitors.length} monitors · checks run inside LabDash · alerts via Pushover`}
        actions={
          <>
            <button className="btn-outline py-1.5" onClick={() => setMEditor({ open: true, win: null })}>
              <CalendarClock size={15} /> Maintenance
            </button>
            <button className="btn-primary py-1.5" onClick={() => setEditor({ open: true, monitor: null })}>
              <Plus size={15} /> Add monitor
            </button>
          </>
        }
      />

      <div className="grid gap-5 p-6">
        {paused && (
          <div className="flex items-center gap-3 rounded-md border border-warn/40 bg-warn/10 px-4 py-2.5 text-sm">
            <BellOff size={16} className="text-warn" />
            <span className="flex-1">All alerts are paused. Monitors still run, but nothing is sent to Pushover.</span>
            <button className="btn-outline py-1 text-xs" onClick={() => patch.mutate({ alerts_paused: false })}>
              Resume alerts
            </button>
          </div>
        )}
        {activeMaint.map((w) => (
          <div key={w.id} className="flex items-center gap-3 rounded-md border border-maint/40 bg-maint/10 px-4 py-2.5 text-sm">
            <Wrench size={16} className="text-maint" />
            <span className="flex-1">
              <b>{w.name}</b> is in progress · {w.scope === "global" ? "all monitors" : w.scopeName} · alerts suppressed
            </span>
          </div>
        ))}

        <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
          <Stat label="Up" value={counts.up ?? 0} tone="text-up" />
          <Stat label="Down" value={counts.down ?? 0} tone={counts.down ? "text-down" : "text-fg"} />
          <Stat label="Pending" value={counts.pending ?? 0} tone={counts.pending ? "text-warn" : "text-fg"} />
          <Stat label="Maintenance / paused" value={(counts.maintenance ?? 0) + (counts.paused ?? 0)} tone="text-fg" />
          <Stat label="Avg uptime (24h)" value={pct(avg24)} tone="text-fg" />
        </div>

        <div className="flex gap-1 border-b border-line">
          {(["monitors", "incidents", "maintenance"] as Tab[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={clsx("-mb-px border-b-2 px-3 py-2 text-sm capitalize transition", tab === t ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg")}
            >
              {t}
            </button>
          ))}
        </div>

        {tab === "monitors" &&
          (isLoading ? null : monitors.length === 0 ? (
            <Empty onAdd={() => setEditor({ open: true, monitor: null })} />
          ) : (
            <div className="overflow-x-auto rounded-lg border border-line">
              <table className="w-full text-[13px]">
                <thead className="border-b border-line bg-panel text-xs text-muted">
                  <tr>
                    <th className="w-8" />
                    <th className="px-3 py-2 text-left font-medium">Monitor</th>
                    <th className="px-3 py-2 text-left font-medium">Status</th>
                    <th className="px-3 py-2 text-left font-medium">Last 24 hours</th>
                    <th className="px-3 py-2 text-right font-medium">24h</th>
                    <th className="px-3 py-2 text-right font-medium">7d</th>
                    <th className="px-3 py-2 text-right font-medium">30d</th>
                    <th className="px-3 py-2 text-right font-medium">Latency</th>
                    <th className="w-28" />
                  </tr>
                </thead>
                <tbody>
                  {sorted.map((m) => (
                    <Fragment key={m.id}>
                      <MonitorRow monitor={m} expanded={expanded === m.id} onToggle={() => setExpanded(expanded === m.id ? null : m.id)} onEdit={() => setEditor({ open: true, monitor: m })} />
                      {expanded === m.id && (
                        <tr className="border-b border-line bg-panel/60">
                          <td colSpan={9} className="px-6 py-4">
                            <MonitorDetail monitor={m} />
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  ))}
                </tbody>
              </table>
            </div>
          ))}

        {tab === "incidents" && <IncidentList />}
        {tab === "maintenance" && <MaintenanceList windows={maint} onEdit={(w) => setMEditor({ open: true, win: w })} />}
      </div>

      <MonitorEditor open={editor.open} monitor={editor.monitor} onClose={() => setEditor((e) => ({ ...e, open: false }))} />
      <MaintenanceEditor open={mEditor.open} window={mEditor.win} onClose={() => setMEditor((e) => ({ ...e, open: false }))} />
    </>
  );
}

function Stat({ label, value, tone }: { label: string; value: string | number; tone: string }) {
  return (
    <div className="rounded-lg border border-line bg-panel px-4 py-3">
      <div className="text-[11px] font-medium tracking-wide text-muted uppercase">{label}</div>
      <div className={clsx("mt-1 text-2xl font-semibold tabular-nums", tone)}>{value}</div>
    </div>
  );
}

function MonitorRow({ monitor: m, expanded, onToggle, onEdit }: { monitor: Monitor; expanded: boolean; onToggle: () => void; onEdit: () => void }) {
  const check = useCheckMonitor();
  const save = useSaveMonitor();
  const name = m.name || m.serviceName || m.resolvedTarget;
  return (
    <tr className={clsx("cursor-pointer border-b border-line/70 hover:bg-white/[0.03]", expanded && "bg-white/[0.03]")} onClick={onToggle}>
      <td className="pl-3">
        <ChevronRight size={14} className={clsx("text-faint transition-transform", expanded && "rotate-90")} />
      </td>
      <td className="px-3 py-2.5">
        <div className="flex items-center gap-2.5">
          <MonitorIcon serviceId={m.serviceId} icon={m.serviceIcon} name={m.serviceName || name} kind={m.kind} className="size-6 shrink-0" />
          <div className="min-w-0">
            <div className="flex items-center gap-2 font-medium">
              <span className="truncate">{name}</span>
              <span className="rounded bg-white/5 px-1.5 py-px font-mono text-[10px] text-muted uppercase">{m.kind}</span>
              {m.muted && <BellOff size={12} className="text-faint" aria-label="Muted" />}
            </div>
            <div className="max-w-[260px] truncate font-mono text-[11px] text-faint" title={m.resolvedTarget}>
              {m.resolvedTarget || "no target"}
            </div>
          </div>
        </div>
      </td>
      <td className="px-3 py-2.5">
        <div className="flex items-center gap-2">
          <StatusDot status={m.status} />
          <span className={clsx("font-medium", statusText[m.status])}>{statusLabel(m.status)}</span>
        </div>
        <div className="mt-0.5 max-w-[200px] truncate text-[11px] text-faint" title={m.lastMessage}>
          {m.lastChangeAt ? `for ${duration(Date.now() - Date.parse(m.lastChangeAt))}` : ago(m.lastCheckAt)}
          {m.status === "down" && m.lastMessage ? ` · ${m.lastMessage}` : ""}
        </div>
      </td>
      <td className="w-[300px] px-3 py-2.5">
        <UptimeBars buckets={m.bars} />
      </td>
      <td className="px-3 py-2.5 text-right tabular-nums">{pct(m.uptime24h)}</td>
      <td className="px-3 py-2.5 text-right text-muted tabular-nums">{pct(m.uptime7d)}</td>
      <td className="px-3 py-2.5 text-right text-muted tabular-nums">{pct(m.uptime30d)}</td>
      <td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">{m.lastLatencyMs != null ? `${m.lastLatencyMs} ms` : "—"}</td>
      <td className="px-3 py-2.5" onClick={(e) => e.stopPropagation()}>
        <div className="flex justify-end gap-0.5">
          <button className="btn-ghost p-1.5" title="Check now" onClick={() => check.mutate(m.id)} disabled={check.isPending}>
            <RefreshCw size={14} className={clsx(check.isPending && "animate-spin")} />
          </button>
          <button className="btn-ghost p-1.5" title={m.muted ? "Unmute alerts" : "Mute alerts"} onClick={() => save.mutate({ id: m.id, muted: !m.muted })}>
            {m.muted ? <BellOff size={14} className="text-warn" /> : <Bell size={14} />}
          </button>
          <button className="btn-ghost p-1.5" title="Edit" onClick={onEdit}>
            <Pencil size={14} />
          </button>
        </div>
      </td>
    </tr>
  );
}

function MonitorDetail({ monitor: m }: { monitor: Monitor }) {
  const [range, setRange] = useState<HistoryRange>("24h");
  const { data } = useMonitorHistory(m.id, range);
  return (
    <div className="grid gap-4">
      <div className="flex items-center gap-4 text-xs text-muted">
        <span>
          Checked every <b className="text-fg">{m.intervalS}s</b> · timeout {m.timeoutS}s · down after {m.retries + 1} failed checks
        </span>
        <span>
          Last check <b className="text-fg">{ago(m.lastCheckAt)}</b>: {m.lastMessage || "—"}
        </span>
        <div className="ml-auto flex rounded-md border border-line-2 p-0.5">
          {(["24h", "7d", "30d"] as HistoryRange[]).map((r) => (
            <button key={r} onClick={() => setRange(r)} className={clsx("rounded px-2.5 py-1 text-xs", range === r ? "bg-accent/20 text-fg" : "text-muted hover:text-fg")}>
              {r}
            </button>
          ))}
        </div>
      </div>
      {data && (
        <>
          <div>
            <div className="mb-1.5 text-[11px] font-medium tracking-wide text-muted uppercase">Availability</div>
            <UptimeBars buckets={data.buckets} bucketSeconds={data.bucketSeconds} height={30} />
          </div>
          <div>
            <div className="mb-1.5 text-[11px] font-medium tracking-wide text-muted uppercase">Response time</div>
            <LatencyChart buckets={data.buckets} />
          </div>
          <div>
            <div className="mb-1.5 text-[11px] font-medium tracking-wide text-muted uppercase">Recent incidents</div>
            {data.incidents.length === 0 ? <p className="text-xs text-faint">No incidents recorded. 🎉</p> : <IncidentTable incidents={data.incidents} compact />}
          </div>
        </>
      )}
    </div>
  );
}

function IncidentList() {
  const { data = [] } = useIncidents();
  if (data.length === 0) return <p className="py-10 text-center text-sm text-muted">No incidents yet.</p>;
  return <IncidentTable incidents={data} />;
}

function IncidentTable({ incidents, compact }: { incidents: Incident[]; compact?: boolean }) {
  return (
    <div className="overflow-hidden rounded-lg border border-line">
      <table className="w-full text-[13px]">
        <tbody>
          {incidents.map((i) => {
            const ongoing = !i.endedAt;
            const dur = (i.endedAt ? Date.parse(i.endedAt) : Date.now()) - Date.parse(i.startedAt);
            return (
              <tr key={i.id} className="border-b border-line/70 last:border-0">
                {!compact && (
                  <td className="w-56 px-3 py-2">
                    <div className="flex items-center gap-2">
                      <MonitorIcon serviceId={i.serviceId} icon={i.serviceIcon} name={i.monitorName} className="size-5" />
                      <span className="font-medium">{i.monitorName}</span>
                    </div>
                  </td>
                )}
                <td className="w-28 px-3 py-2">
                  <span className={clsx("rounded px-1.5 py-0.5 text-[11px] font-semibold", ongoing ? "bg-down/20 text-down" : "bg-white/5 text-muted")}>{ongoing ? "Ongoing" : "Resolved"}</span>
                </td>
                <td className="w-44 px-3 py-2 text-muted">{fmtDateTime(i.startedAt)}</td>
                <td className="w-28 px-3 py-2 tabular-nums">{duration(dur)}</td>
                <td className="truncate px-3 py-2 font-mono text-xs text-muted">{i.cause}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function MaintenanceList({ windows, onEdit }: { windows: Maintenance[]; onEdit: (w: Maintenance) => void }) {
  if (windows.length === 0) return <p className="py-10 text-center text-sm text-muted">No maintenance windows. Schedule one to silence alerts during planned work.</p>;
  return (
    <div className="overflow-hidden rounded-lg border border-line">
      <table className="w-full text-[13px]">
        <tbody>
          {windows.map((w) => (
            <tr key={w.id} onClick={() => onEdit(w)} className="cursor-pointer border-b border-line/70 last:border-0 hover:bg-white/[0.03]">
              <td className="px-3 py-2.5 font-medium">{w.name}</td>
              <td className="px-3 py-2.5 text-muted">{w.scope === "global" ? "All monitors" : w.scopeName || `${w.scope} #${w.scopeId}`}</td>
              <td className="px-3 py-2.5 font-mono text-xs text-muted">
                {w.recurrence ? `cron ${w.recurrence} · ${w.durationMin} min` : `${fmtDateTime(w.startsAt)} → ${fmtDateTime(w.endsAt)}`}
              </td>
              <td className="px-3 py-2.5 text-right">
                {w.active ? (
                  <span className="rounded bg-maint/20 px-1.5 py-0.5 text-[11px] font-semibold text-maint">Active now</span>
                ) : !w.enabled ? (
                  <span className="text-[11px] text-faint">Disabled</span>
                ) : (
                  <span className="text-[11px] text-faint">Scheduled</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Empty({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="rounded-lg border border-dashed border-line-2 p-10 text-center">
      <p className="text-sm text-muted">No monitors yet. Add one for a service, or monitor any URL, port, host or DNS name.</p>
      <button className="btn-primary mt-4" onClick={onAdd}>
        <Plus size={15} /> Add monitor
      </button>
    </div>
  );
}
