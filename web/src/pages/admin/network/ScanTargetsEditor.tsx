import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { Modal } from "@/components/Modal";
import { useDeleteScanTarget, useSaveScanTarget, useScanTargets, useSubnets } from "@/lib/queries";
import type { PortProfile, ScanTarget } from "@/lib/types";
import { ago } from "@/lib/time";

const profiles: { id: PortProfile; label: string; hint: string }[] = [
  { id: "top100", label: "Top 100", hint: "fast" },
  { id: "top1000", label: "Top 1000", hint: "recommended" },
  { id: "full", label: "All 65,535", hint: "slow" },
  { id: "custom", label: "Custom", hint: "your list" },
];

const schedules = [
  { v: "", label: "Manual only" },
  { v: "0 * * * *", label: "Hourly" },
  { v: "0 */6 * * *", label: "Every 6 hours" },
  { v: "0 3 * * *", label: "Daily 3am" },
  { v: "0 3 * * 0", label: "Weekly (Sun 3am)" },
];

type Draft = Omit<ScanTarget, "id" | "lastRunAt"> & { id?: number };
const blank: Draft = { cidr: "", name: "", schedule: "0 */6 * * *", portProfile: "top1000", customPorts: "", enabled: true };

/** Manage the subnets the scanner covers, their schedules and port profiles. */
export function ScanTargetsEditor({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { data: targets = [] } = useScanTargets();
  const { data: subnets = [] } = useSubnets();
  const save = useSaveScanTarget();
  const del = useDeleteScanTarget();
  const [draft, setDraft] = useState<Draft | null>(null);
  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setDraft((d) => (d ? { ...d, [k]: v } : d));
  const unscanned = subnets.filter((s) => !targets.some((t) => t.cidr === s.cidr));

  const submit = () => draft && save.mutate(draft, { onSuccess: () => setDraft(null) });
  const custom = schedules.every((s) => s.v !== draft?.schedule);

  return (
    <Modal open={open} onClose={onClose} title="Scan targets" width="max-w-2xl">
      <div className="grid gap-4">
        <p className="text-xs text-muted">
          LabDash runs nmap from its own container (host networking) to find devices, open ports, MAC vendors and OS. The first scan of a target is a baseline and doesn't send new-device alerts.
        </p>
        <div className="overflow-hidden rounded-lg border border-line">
          {targets.length === 0 && <p className="px-4 py-6 text-center text-sm text-muted">No targets yet.</p>}
          {targets.map((t) => (
            <div key={t.id} className="flex items-center gap-3 border-b border-line/70 px-4 py-2.5 text-sm last:border-0">
              <span className={t.enabled ? "font-mono" : "font-mono text-faint line-through"}>{t.cidr}</span>
              <span className="text-muted">{t.name}</span>
              <span className="ml-auto text-xs text-faint">
                {schedules.find((s) => s.v === t.schedule)?.label ?? `cron ${t.schedule}`} · {profiles.find((p) => p.id === t.portProfile)?.label} · last {ago(t.lastRunAt)}
              </span>
              <button className="btn-ghost px-2 py-1 text-xs" onClick={() => setDraft({ ...t })}>
                Edit
              </button>
              <button className="btn-ghost p-1" title="Delete" onClick={() => confirm(`Stop scanning ${t.cidr}? Devices already found are kept.`) && del.mutate(t.id)}>
                <Trash2 size={13} />
              </button>
            </div>
          ))}
        </div>

        {!draft && (
          <div className="flex flex-wrap items-center gap-2">
            <button className="btn-outline py-1.5" onClick={() => setDraft({ ...blank })}>
              <Plus size={14} /> Add target
            </button>
            {unscanned.map((s) => (
              <button key={s.id} className="rounded-md border border-line-2 px-2 py-1 text-xs text-muted hover:text-fg" onClick={() => setDraft({ ...blank, cidr: s.cidr, name: s.name })}>
                + {s.name || s.cidr} <span className="font-mono">{s.cidr}</span>
              </button>
            ))}
          </div>
        )}

        {draft && (
          <div className="grid gap-3 rounded-lg border border-accent/40 bg-accent/5 p-4">
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="label">CIDR or IP</label>
                <input className="input font-mono" autoFocus value={draft.cidr} onChange={(e) => set("cidr", e.target.value)} placeholder="192.168.8.0/24" />
              </div>
              <div>
                <label className="label">Name</label>
                <input className="input" value={draft.name} onChange={(e) => set("name", e.target.value)} placeholder="Servers" />
              </div>
            </div>
            <div>
              <label className="label">Ports</label>
              <div className="grid grid-cols-4 gap-2">
                {profiles.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => set("portProfile", p.id)}
                    className={draft.portProfile === p.id ? "btn flex-col gap-0 border border-accent bg-accent/15 py-1.5 text-fg" : "btn-outline flex-col gap-0 py-1.5"}
                  >
                    <span>{p.label}</span>
                    <span className="text-[10px] text-faint">{p.hint}</span>
                  </button>
                ))}
              </div>
              {draft.portProfile === "custom" && (
                <input className="input mt-2 font-mono" value={draft.customPorts} onChange={(e) => set("customPorts", e.target.value)} placeholder="22,80,443,8000-9000" />
              )}
            </div>
            <div className="grid grid-cols-[1fr_1fr] gap-3">
              <div>
                <label className="label">Schedule</label>
                <select className="input" value={custom ? "custom" : draft.schedule} onChange={(e) => set("schedule", e.target.value === "custom" ? "*/30 * * * *" : e.target.value)}>
                  {schedules.map((s) => (
                    <option key={s.v} value={s.v}>
                      {s.label}
                    </option>
                  ))}
                  <option value="custom">Custom cron…</option>
                </select>
              </div>
              {custom && (
                <div>
                  <label className="label">Cron</label>
                  <input className="input font-mono" value={draft.schedule} onChange={(e) => set("schedule", e.target.value)} />
                </div>
              )}
            </div>
            <label className="flex items-center gap-2 text-sm text-muted">
              <input type="checkbox" className="accent-accent" checked={draft.enabled} onChange={(e) => set("enabled", e.target.checked)} />
              Enabled
            </label>
            <div className="flex items-center gap-2">
              <button className="btn-primary py-1.5" disabled={!draft.cidr.trim() || save.isPending} onClick={submit}>
                Save target
              </button>
              <button className="btn-ghost py-1.5" onClick={() => setDraft(null)}>
                Cancel
              </button>
              {save.error && <span className="text-sm text-down">{save.error.message}</span>}
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
