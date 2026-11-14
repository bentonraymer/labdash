import { useMemo, useState } from "react";
import clsx from "clsx";
import { Modal } from "@/components/Modal";
import { useSaveReminder } from "@/lib/queries";
import type { Reminder, ReminderKind, RepeatUnit } from "@/lib/types";
import { addInterval, fmtWhen, unitLabel } from "@/lib/planning";
import { fromLocalInput, toLocalInput } from "@/lib/time";

const kinds: { id: ReminderKind; label: string; hint: string }[] = [
  { id: "once", label: "One-off", hint: "A single date and time" },
  { id: "calendar", label: "On a schedule", hint: "Fixed dates, e.g. the 1st of every quarter" },
  { id: "after_done", label: "After I do it", hint: "Counted from when you mark it done" },
];

const presets: { label: string; n: number; unit: RepeatUnit }[] = [
  { label: "Weekly", n: 1, unit: "week" },
  { label: "Monthly", n: 1, unit: "month" },
  { label: "Quarterly", n: 1, unit: "quarter" },
  { label: "Every 6 months", n: 6, unit: "month" },
  { label: "Yearly", n: 1, unit: "year" },
];

function defaultStart(): string {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  d.setHours(9, 0, 0, 0);
  return toLocalInput(d.toISOString());
}

export function ReminderEditor({ reminder, onClose }: { reminder: Reminder | null; onClose: () => void }) {
  const save = useSaveReminder();
  const [title, setTitle] = useState(reminder?.title ?? "");
  const [notes, setNotes] = useState(reminder?.notes ?? "");
  const [kind, setKind] = useState<ReminderKind>(reminder?.kind ?? "calendar");
  const [n, setN] = useState(reminder?.everyN ?? 1);
  const [unit, setUnit] = useState<RepeatUnit>(reminder?.unit ?? "quarter");
  const [start, setStart] = useState(reminder ? toLocalInput(reminder.kind === "calendar" ? reminder.anchorAt : reminder.dueAt) : defaultStart());
  const [enabled, setEnabled] = useState(reminder ? reminder.enabled : true);

  // Preview the next few occurrences.
  const preview = useMemo(() => {
    if (!start) return [];
    const first = new Date(start);
    if (Number.isNaN(first.getTime())) return [];
    if (kind === "once") return [first];
    if (kind === "after_done") return [first];
    const out: Date[] = [];
    const now = Date.now();
    for (let k = 0, guard = 0; out.length < 3 && guard < 5000; k++, guard++) {
      const d = addInterval(first, n * k, unit);
      if (d.getTime() > now) out.push(d);
    }
    return out;
  }, [start, kind, n, unit]);

  const submit = () =>
    save.mutate(
      { id: reminder?.id, title, notes, kind, everyN: n, unit, anchorAt: fromLocalInput(start), enabled: reminder ? enabled : undefined },
      { onSuccess: onClose },
    );

  return (
    <Modal
      open
      onClose={onClose}
      title={reminder ? "Edit reminder" : "New reminder"}
      width="max-w-xl"
      footer={
        <>
          {save.error && <span className="mr-auto text-sm text-down">{save.error.message}</span>}
          <button className="btn-ghost py-1.5" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary py-1.5" disabled={!title.trim() || !start || save.isPending} onClick={submit}>
            {reminder ? "Save" : "Create reminder"}
          </button>
        </>
      }
    >
      <div className="grid gap-4">
        <div>
          <label className="label">What</label>
          <input className="input" autoFocus value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Check backups restore correctly" />
        </div>
        <div>
          <label className="label">Notes (sent with the notification)</label>
          <textarea className="input min-h-[70px]" value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="Restore one file from each backup job and open it." />
        </div>

        <div className="grid grid-cols-3 gap-2">
          {kinds.map((k) => (
            <button
              key={k.id}
              type="button"
              onClick={() => setKind(k.id)}
              className={clsx("rounded-md border px-3 py-2 text-left", kind === k.id ? "border-accent bg-accent/10" : "border-line hover:border-line-2")}
            >
              <div className="text-sm font-medium">{k.label}</div>
              <div className="text-[11px] text-muted">{k.hint}</div>
            </button>
          ))}
        </div>

        {kind !== "once" && (
          <div className="grid gap-2">
            <div className="flex flex-wrap gap-1.5">
              {presets.map((p) => (
                <button
                  key={p.label}
                  type="button"
                  onClick={() => {
                    setN(p.n);
                    setUnit(p.unit);
                  }}
                  className={clsx("rounded-full border px-2.5 py-0.5 text-xs", n === p.n && unit === p.unit ? "border-accent bg-accent/15 text-fg" : "border-line-2 text-muted hover:text-fg")}
                >
                  {p.label}
                </button>
              ))}
            </div>
            <div className="flex items-center gap-2 text-sm">
              <span className="text-muted">{kind === "after_done" ? "Remind me" : "Every"}</span>
              <input type="number" min={1} max={1000} className="input w-20" value={n} onChange={(e) => setN(Math.max(1, Number(e.target.value) || 1))} />
              <select className="input w-auto" value={unit} onChange={(e) => setUnit(e.target.value as RepeatUnit)}>
                {(Object.keys(unitLabel) as RepeatUnit[]).map((u) => (
                  <option key={u} value={u}>
                    {unitLabel[u][n === 1 ? 0 : 1]}
                  </option>
                ))}
              </select>
              {kind === "after_done" && <span className="text-muted">after I mark it done</span>}
            </div>
          </div>
        )}

        <div>
          <label className="label">{kind === "once" ? "When" : kind === "calendar" ? "Starting" : "First reminder"}</label>
          <input type="datetime-local" className="input w-auto" value={start} onChange={(e) => setStart(e.target.value)} />
          {kind === "calendar" && <p className="mt-1 text-[11px] text-faint">Sets the day and time it repeats on, e.g. the 1st at 9:00.</p>}
          {kind === "after_done" && <p className="mt-1 text-[11px] text-faint">After this, the next one is counted from the day you mark it done, at this time of day.</p>}
        </div>

        {preview.length > 0 && (
          <div className="rounded-md border border-line bg-black/20 px-3 py-2 text-xs text-muted">
            {kind === "calendar" ? "Next: " : "Due: "}
            {preview.map((d) => fmtWhen(d.toISOString())).join(" · ")}
            {kind === "calendar" && "…"}
          </div>
        )}

        {reminder && (
          <label className="flex items-center gap-2 text-sm text-muted">
            <input type="checkbox" className="accent-accent" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            {reminder.state === "done" ? "Active again (it's finished; tick to bring it back)" : "Active"}
          </label>
        )}
      </div>
    </Modal>
  );
}
