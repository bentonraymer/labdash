import type { RepeatUnit, Todo } from "./types";

/** "in 3 days", "in 5 hours", "2 days ago", "today". */
export function relTime(iso: string, now = Date.now()): string {
  const ms = Date.parse(iso) - now;
  const abs = Math.abs(ms);
  const fmt = (n: number, unit: string) => `${n} ${unit}${n === 1 ? "" : "s"}`;
  let s: string;
  if (abs < 60_000) return "now";
  if (abs < 3_600_000) s = fmt(Math.round(abs / 60_000), "minute");
  else if (abs < 86_400_000) s = fmt(Math.round(abs / 3_600_000), "hour");
  else if (abs < 60 * 86_400_000) s = fmt(Math.round(abs / 86_400_000), "day");
  else s = fmt(Math.round(abs / (30 * 86_400_000)), "month");
  return ms > 0 ? `in ${s}` : `${s} ago`;
}

export const fmtWhen = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { weekday: "short", month: "short", day: "numeric", year: new Date(iso).getFullYear() === new Date().getFullYear() ? undefined : "numeric", hour: "numeric", minute: "2-digit" });

export const unitLabel: Record<RepeatUnit, [string, string]> = {
  day: ["day", "days"],
  week: ["week", "weeks"],
  month: ["month", "months"],
  quarter: ["quarter", "quarters"],
  year: ["year", "years"],
};

/** Mirror of the server's month-clamped stepping, for previews. */
export function addInterval(d: Date, n: number, unit: RepeatUnit): Date {
  const out = new Date(d);
  if (unit === "day") out.setDate(out.getDate() + n);
  else if (unit === "week") out.setDate(out.getDate() + 7 * n);
  else {
    const months = unit === "month" ? n : unit === "quarter" ? 3 * n : 12 * n;
    const day = out.getDate();
    out.setDate(1);
    out.setMonth(out.getMonth() + months);
    const last = new Date(out.getFullYear(), out.getMonth() + 1, 0).getDate();
    out.setDate(Math.min(day, last));
  }
  return out;
}

// ── todos ─────────────────────────────────────────────────────────────────

export const priorities = [
  { p: 1, label: "Urgent", short: "P1", cls: "bg-red-500/20 text-red-300 border-red-500/40" },
  { p: 2, label: "High", short: "P2", cls: "bg-orange-500/15 text-orange-300 border-orange-500/30" },
  { p: 3, label: "Normal", short: "P3", cls: "bg-white/5 text-muted border-line-2" },
  { p: 4, label: "Low", short: "P4", cls: "bg-transparent text-faint border-line" },
] as const;

export const todayISO = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};

/** Due-date chip text and tone for a todo. */
export function dueInfo(t: Pick<Todo, "dueDate" | "status">): { text: string; tone: "overdue" | "today" | "soon" | "later" } | null {
  if (!t.dueDate) return null;
  const today = todayISO();
  const days = Math.round((Date.parse(`${t.dueDate}T12:00:00`) - Date.parse(`${today}T12:00:00`)) / 86_400_000);
  const date = new Date(`${t.dueDate}T12:00:00`).toLocaleDateString(undefined, { month: "short", day: "numeric", year: days > 300 || days < -300 ? "numeric" : undefined });
  if (t.status === "done") return { text: date, tone: "later" };
  if (days < 0) return { text: `${date} · ${-days}d overdue`, tone: "overdue" };
  if (days === 0) return { text: "Today", tone: "today" };
  if (days === 1) return { text: "Tomorrow", tone: "soon" };
  if (days < 7) return { text: new Date(`${t.dueDate}T12:00:00`).toLocaleDateString(undefined, { weekday: "long" }), tone: "soon" };
  return { text: date, tone: "later" };
}

export const dueTone = {
  overdue: "text-down",
  today: "text-warn",
  soon: "text-fg",
  later: "text-muted",
} as const;

/** Link to the todos for a service or host. */
export const todosLink = (q: { service?: number; host?: number }) => `/todos?${q.service ? `service=${q.service}` : `host=${q.host}`}`;
