import { useEffect, useState, type FormEvent } from "react";
import { Trash2 } from "lucide-react";
import { Modal } from "@/components/Modal";
import { useDeleteMaintenance, useHosts, useLaunchpad, useSaveMaintenance } from "@/lib/queries";
import type { Maintenance } from "@/lib/types";
import { fromLocalInput, toLocalInput } from "@/lib/time";

const presets = [
  { label: "Every Sunday 3am", cron: "0 3 * * 0" },
  { label: "Daily 4am", cron: "0 4 * * *" },
  { label: "1st of month 2am", cron: "0 2 1 * *" },
];

export function MaintenanceEditor({ open, onClose, window: win }: { open: boolean; onClose: () => void; window: Maintenance | null }) {
  const { data } = useLaunchpad();
  const { data: hosts = [] } = useHosts();
  const save = useSaveMaintenance();
  const del = useDeleteMaintenance();
  const [name, setName] = useState("");
  const [scope, setScope] = useState<Maintenance["scope"]>("global");
  const [scopeId, setScopeId] = useState("");
  const [recurring, setRecurring] = useState(false);
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [cron, setCron] = useState("0 3 * * 0");
  const [duration, setDuration] = useState(60);
  const [enabled, setEnabled] = useState(true);

  useEffect(() => {
    if (!open) return;
    save.reset();
    const now = new Date();
    const inHour = new Date(now.getTime() + 3600_000);
    setName(win?.name ?? "");
    setScope(win?.scope ?? "global");
    setScopeId(win?.scopeId ? String(win.scopeId) : "");
    setRecurring(!!win?.recurrence);
    setStart(toLocalInput(win?.startsAt || now.toISOString()));
    setEnd(toLocalInput(win?.endsAt || inHour.toISOString()));
    setCron(win?.recurrence || "0 3 * * 0");
    setDuration(win?.durationMin ?? 60);
    setEnabled(win?.enabled ?? true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, win]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(
      {
        id: win?.id,
        name,
        scope,
        scopeId: scope === "global" ? null : Number(scopeId) || null,
        startsAt: recurring ? "" : fromLocalInput(start),
        endsAt: recurring ? "" : fromLocalInput(end),
        recurrence: recurring ? cron : "",
        durationMin: duration,
        enabled,
      },
      { onSuccess: onClose },
    );
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={win ? "Edit maintenance window" : "Schedule maintenance"}
      footer={
        <>
          {win && (
            <button type="button" className="btn-danger mr-auto" onClick={() => del.mutate(win.id, { onSuccess: onClose })}>
              <Trash2 size={15} /> Delete
            </button>
          )}
          {save.error && <span className="mr-2 text-sm text-down">{save.error.message}</span>}
          <button type="button" className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" form="maint-form" className="btn-primary" disabled={!name.trim() || save.isPending}>
            Save
          </button>
        </>
      }
    >
      <form id="maint-form" onSubmit={submit} className="grid gap-4">
        <p className="text-xs text-muted">During a window, checks keep running but monitors show “maintenance” and no alerts are sent.</p>
        <div>
          <label className="label">Name</label>
          <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Proxmox updates" />
        </div>
        <div className="grid grid-cols-[200px_1fr] gap-3">
          <div>
            <label className="label">Applies to</label>
            <select className="input" value={scope} onChange={(e) => setScope(e.target.value as Maintenance["scope"])}>
              <option value="global">Everything</option>
              <option value="service">One service</option>
              <option value="host">A host (and everything on it)</option>
            </select>
          </div>
          {scope === "host" && (
            <div>
              <label className="label">Host</label>
              <select className="input" value={scopeId} onChange={(e) => setScopeId(e.target.value)}>
                <option value="">Choose…</option>
                {hosts.map((h) => (
                  <option key={h.id} value={h.id}>
                    {h.name}
                  </option>
                ))}
              </select>
            </div>
          )}
          {scope === "service" && (
            <div>
              <label className="label">Service</label>
              <select className="input" value={scopeId} onChange={(e) => setScopeId(e.target.value)}>
                <option value="">Choose…</option>
                {data?.services
                  .slice()
                  .sort((a, b) => a.name.localeCompare(b.name))
                  .map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
              </select>
            </div>
          )}
        </div>

        <div className="flex gap-2">
          <button type="button" onClick={() => setRecurring(false)} className={!recurring ? "btn border border-accent bg-accent/15 text-fg" : "btn-outline"}>
            One-off
          </button>
          <button type="button" onClick={() => setRecurring(true)} className={recurring ? "btn border border-accent bg-accent/15 text-fg" : "btn-outline"}>
            Recurring
          </button>
        </div>

        {!recurring ? (
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">Starts</label>
              <input type="datetime-local" className="input" value={start} onChange={(e) => setStart(e.target.value)} />
            </div>
            <div>
              <label className="label">Ends</label>
              <input type="datetime-local" className="input" value={end} onChange={(e) => setEnd(e.target.value)} />
            </div>
          </div>
        ) : (
          <div className="grid gap-3">
            <div className="grid grid-cols-[1fr_140px] gap-3">
              <div>
                <label className="label">Starts on (cron)</label>
                <input className="input font-mono" value={cron} onChange={(e) => setCron(e.target.value)} placeholder="0 3 * * 0" />
              </div>
              <div>
                <label className="label">Duration (min)</label>
                <input className="input font-mono" inputMode="numeric" value={duration} onChange={(e) => setDuration(Number(e.target.value.replace(/\D/g, "")) || 0)} />
              </div>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {presets.map((p) => (
                <button type="button" key={p.cron} onClick={() => setCron(p.cron)} className="rounded-md border border-line-2 px-2 py-1 text-xs text-muted hover:text-fg">
                  {p.label}
                </button>
              ))}
            </div>
            <p className="text-xs text-faint">Minute hour day-of-month month day-of-week, in the server's time zone (TZ in docker-compose).</p>
          </div>
        )}

        <label className="flex items-center gap-2 text-sm text-muted">
          <input type="checkbox" className="accent-accent" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          Enabled
        </label>
      </form>
    </Modal>
  );
}
