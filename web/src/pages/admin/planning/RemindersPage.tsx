import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import clsx from "clsx";
import { AlarmClock, BellRing, Check, ChevronDown, Clock, Pencil, Plus, Repeat, Send, SkipForward, Trash2 } from "lucide-react";
import { useDeleteReminder, useReminderAction, useReminders } from "@/lib/queries";
import type { Reminder } from "@/lib/types";
import { fmtWhen, relTime } from "@/lib/planning";
import { PageHeader } from "../AdminLayout";
import { ReminderEditor } from "./ReminderEditor";

/** One-off and recurring reminders, sent by Pushover until marked done. */
export function RemindersPage() {
  const { data = [], isLoading } = useReminders();
  const [params, setParams] = useSearchParams();
  const focus = Number(params.get("r")) || null;
  const [editing, setEditing] = useState<Reminder | "new" | null>(null);
  const [showInactive, setShowInactive] = useState(false);

  const groups = useMemo(() => {
    const due = data.filter((r) => r.state === "due" || r.state === "snoozed").sort((a, b) => a.dueAt.localeCompare(b.dueAt));
    const upcoming = data.filter((r) => r.state === "upcoming").sort((a, b) => a.dueAt.localeCompare(b.dueAt));
    const inactive = data.filter((r) => r.state === "paused" || r.state === "done");
    return { due, upcoming, inactive };
  }, [data]);

  useEffect(() => {
    if (focus && data.some((r) => r.id === focus && (r.state === "paused" || r.state === "done"))) setShowInactive(true);
  }, [focus, data]);

  return (
    <>
      <PageHeader
        title="Reminders"
        subtitle="Sent by Pushover when due, then once a day until you mark them done."
        actions={
          <button className="btn-primary py-1.5" onClick={() => setEditing("new")}>
            <Plus size={14} /> New reminder
          </button>
        }
      />
      <div className="grid max-w-4xl gap-6 p-6">
        {!isLoading && data.length === 0 && (
          <div className="grid justify-items-center gap-2 rounded-lg border border-dashed border-line-2 px-6 py-10 text-center text-sm text-muted">
            <AlarmClock size={26} className="text-faint" />
            No reminders yet. Try one for the chores that are easy to forget, like checking backups each quarter or renewing a domain.
            <button className="btn-outline mt-2 py-1.5" onClick={() => setEditing("new")}>
              <Plus size={14} /> New reminder
            </button>
          </div>
        )}

        {groups.due.length > 0 && (
          <Section title="Due now" count={groups.due.length} tone="text-warn">
            {groups.due.map((r) => (
              <ReminderRow key={r.id} r={r} focused={focus === r.id} onEdit={() => setEditing(r)} />
            ))}
          </Section>
        )}
        {groups.upcoming.length > 0 && (
          <Section title="Upcoming" count={groups.upcoming.length}>
            {groups.upcoming.map((r) => (
              <ReminderRow key={r.id} r={r} focused={focus === r.id} onEdit={() => setEditing(r)} />
            ))}
          </Section>
        )}
        {groups.inactive.length > 0 && (
          <div>
            <button className="mb-2 flex items-center gap-1.5 text-xs text-muted hover:text-fg" onClick={() => setShowInactive(!showInactive)}>
              <ChevronDown size={13} className={clsx("transition-transform", !showInactive && "-rotate-90")} />
              Paused & finished ({groups.inactive.length})
            </button>
            {showInactive && (
              <div className="grid gap-2">
                {groups.inactive.map((r) => (
                  <ReminderRow key={r.id} r={r} focused={focus === r.id} onEdit={() => setEditing(r)} />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
      {editing && (
        <ReminderEditor
          reminder={editing === "new" ? null : editing}
          onClose={() => {
            setEditing(null);
            if (focus) setParams({}, { replace: true });
          }}
        />
      )}
    </>
  );
}

function Section({ title, count, tone, children }: { title: string; count: number; tone?: string; children: React.ReactNode }) {
  return (
    <section>
      <h2 className={clsx("mb-2 text-[11px] font-semibold tracking-widest uppercase", tone ?? "text-faint")}>
        {title} <span className="text-faint">· {count}</span>
      </h2>
      <div className="grid gap-2">{children}</div>
    </section>
  );
}

function snoozeTimes(): { label: string; at: Date }[] {
  const now = new Date();
  const tomorrow9 = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, 9, 0);
  const nextMon = new Date(now.getFullYear(), now.getMonth(), now.getDate() + ((8 - now.getDay()) % 7 || 7), 9, 0);
  return [
    { label: "1 hour", at: new Date(now.getTime() + 3_600_000) },
    { label: "3 hours", at: new Date(now.getTime() + 3 * 3_600_000) },
    { label: "Tomorrow 9am", at: tomorrow9 },
    { label: "Next Monday 9am", at: nextMon },
  ];
}

function ReminderRow({ r, focused, onEdit }: { r: Reminder; focused: boolean; onEdit: () => void }) {
  const act = useReminderAction();
  const del = useDeleteReminder();
  const [snoozeOpen, setSnoozeOpen] = useState(false);
  const [notice, setNotice] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (focused) ref.current?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [focused]);
  const recurring = r.kind !== "once";
  const active = r.state === "due" || r.state === "snoozed";
  const lastDone = r.events.find((e) => e.kind === "done");

  return (
    <div
      ref={ref}
      className={clsx(
        "grid gap-2 rounded-lg border bg-panel px-4 py-3",
        focused ? "border-accent ring-2 ring-accent/30" : r.state === "due" ? "border-warn/40" : "border-line",
        (r.state === "paused" || r.state === "done") && "opacity-60",
      )}
    >
      <div className="flex flex-wrap items-start gap-x-3 gap-y-2">
        <div className={clsx("mt-0.5 grid size-8 shrink-0 place-items-center rounded-full", r.state === "due" ? "bg-warn/15 text-warn" : "bg-white/5 text-muted")}>
          {r.state === "due" ? <BellRing size={15} /> : r.state === "snoozed" ? <Clock size={15} /> : recurring ? <Repeat size={15} /> : <AlarmClock size={15} />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{r.title}</span>
            <span className="rounded bg-white/5 px-1.5 py-px text-[11px] text-muted">{r.schedule}</span>
          </div>
          <div className="mt-0.5 text-xs text-muted">
            {r.state === "done" ? (
              <>Done {lastDone ? relTime(lastDone.ts) : ""}</>
            ) : r.state === "paused" ? (
              <>Paused</>
            ) : r.state === "snoozed" && r.snoozedUntil ? (
              <>
                Snoozed until <b className="text-fg">{fmtWhen(r.snoozedUntil)}</b> · was due {relTime(r.dueAt)}
              </>
            ) : r.state === "due" ? (
              <>
                <b className="text-warn">Due {relTime(r.dueAt) === "now" ? "now" : relTime(r.dueAt)}</b> · {fmtWhen(r.dueAt)}
                {r.lastSentAt && <> · last sent {relTime(r.lastSentAt)}</>}
              </>
            ) : (
              <>
                Next <b className="text-fg">{fmtWhen(r.dueAt)}</b> · {relTime(r.dueAt)}
              </>
            )}
            {recurring && lastDone && r.state !== "done" && <> · last done {relTime(lastDone.ts)}</>}
          </div>
          {r.notes && <p className="mt-1.5 line-clamp-2 text-xs whitespace-pre-line text-faint">{r.notes}</p>}
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {active && (
            <button className="btn-primary py-1 text-xs" disabled={act.isPending} onClick={() => act.mutate({ id: r.id, action: "done" })}>
              <Check size={13} /> Done
            </button>
          )}
          {r.state === "upcoming" && (
            <button
              className="btn-outline py-1 text-xs"
              disabled={act.isPending}
              onClick={() =>
                confirm(
                  r.kind === "calendar"
                    ? `Mark "${r.title}" done early? The ${fmtWhen(r.dueAt)} reminder is skipped and the next one is the occurrence after it.`
                    : r.kind === "after_done"
                      ? `Mark "${r.title}" done now? The next reminder is counted from today.`
                      : `Mark "${r.title}" done now? It won't be sent.`,
                ) && act.mutate({ id: r.id, action: "done" })
              }
              title={recurring ? "Done early: moves on to the next occurrence" : "Mark done now"}
            >
              <Check size={13} /> Done early
            </button>
          )}
          {active && (
            <div className="relative">
              <button className="btn-outline py-1 text-xs" onClick={() => setSnoozeOpen(!snoozeOpen)}>
                <Clock size={13} /> Snooze
              </button>
              {snoozeOpen && (
                <div className="absolute right-0 z-20 mt-1 grid w-40 rounded-md border border-line-2 bg-panel-2 py-1 text-xs shadow-xl">
                  {snoozeTimes().map((o) => (
                    <button
                      key={o.label}
                      className="px-3 py-1.5 text-left hover:bg-white/5"
                      onClick={() => {
                        setSnoozeOpen(false);
                        act.mutate({ id: r.id, action: "snooze", until: o.at.toISOString() });
                      }}
                    >
                      {o.label}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          {recurring && active && (
            <button className="btn-ghost px-2 py-1 text-xs" title="Skip this one and move to the next occurrence" onClick={() => act.mutate({ id: r.id, action: "skip" })}>
              <SkipForward size={13} /> Skip
            </button>
          )}
          <button className="btn-ghost p-1.5" title="Edit" onClick={onEdit}>
            <Pencil size={13} />
          </button>
          <button
            className="btn-ghost p-1.5"
            title="Send a test notification now"
            onClick={() =>
              act.mutate(
                { id: r.id, action: "test" },
                { onSuccess: () => setNotice("Test sent."), onError: (e) => setNotice((e as Error).message) },
              )
            }
          >
            <Send size={13} />
          </button>
          <button className="btn-ghost p-1.5" title="Delete" onClick={() => confirm(`Delete "${r.title}"?`) && del.mutate(r.id)}>
            <Trash2 size={13} />
          </button>
        </div>
      </div>
      {(notice || act.error) && <div className={clsx("text-xs", act.error ? "text-down" : "text-up")}>{act.error ? act.error.message : notice}</div>}
      {r.events.length > 0 && recurring && (
        <div className="flex flex-wrap gap-1.5 pl-11 text-[10px] text-faint">
          {r.events.slice(0, 6).map((e) => (
            <span key={e.ts} className="rounded border border-line px-1.5 py-px" title={`${e.kind === "done" ? "Done" : "Skipped"} ${fmtWhen(e.ts)}`}>
              {e.kind === "done" ? "✓" : "↷"} {new Date(e.ts).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "2-digit" })}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
