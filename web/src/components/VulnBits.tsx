import { useState } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { Check, ChevronRight, EyeOff, ExternalLink, RotateCcw, ShieldAlert } from "lucide-react";
import { useUpdateVuln } from "@/lib/queries";
import type { Severity, VulnFinding } from "@/lib/types";
import { ago, fmtDateTime } from "@/lib/time";

export const severities: Severity[] = ["critical", "high", "medium", "low", "info"];

export const sevStyle: Record<Severity, { badge: string; text: string; dot: string }> = {
  critical: { badge: "bg-red-600 text-white", text: "text-red-400", dot: "bg-red-500" },
  high: { badge: "bg-orange-500 text-black", text: "text-orange-400", dot: "bg-orange-500" },
  medium: { badge: "bg-amber-300 text-black", text: "text-amber-300", dot: "bg-amber-300" },
  low: { badge: "bg-sky-500/80 text-white", text: "text-sky-400", dot: "bg-sky-400" },
  info: { badge: "bg-white/15 text-muted", text: "text-muted", dot: "bg-white/40" },
};

export const sourceLabel: Record<VulnFinding["source"], string> = {
  cve: "Known CVE (version)",
  nuclei: "Web check (Nuclei)",
  config: "Config / TLS check",
};

export function SeverityBadge({ severity, className }: { severity: Severity; className?: string }) {
  return <span className={clsx("inline-block rounded px-1.5 py-px text-[10px] font-bold tracking-wide uppercase", sevStyle[severity]?.badge, className)}>{severity}</span>;
}

/** Compact "2 C · 1 H" style counts (only the non-zero ones). */
export function VulnCounts({ counts, linkTo, min = "low" }: { counts?: Partial<Record<Severity, number>>; linkTo?: string; min?: Severity }) {
  if (!counts) return null;
  const cut = severities.indexOf(min);
  const shown = severities.filter((s, i) => i <= cut && (counts[s] ?? 0) > 0);
  if (shown.length === 0) return null;
  const body = (
    <span className="inline-flex items-center gap-1" title={shown.map((s) => `${counts[s]} ${s}`).join(", ")}>
      <ShieldAlert size={12} className={sevStyle[shown[0]].text} />
      {shown.map((s) => (
        <span key={s} className={clsx("rounded px-1 text-[10px] font-semibold", sevStyle[s].badge)}>
          {counts[s]}
          {s[0].toUpperCase()}
        </span>
      ))}
    </span>
  );
  return linkTo ? (
    <Link to={linkTo} onClick={(e) => e.stopPropagation()} className="hover:opacity-80">
      {body}
    </Link>
  ) : (
    body
  );
}

const statusStyle: Record<VulnFinding["status"], string> = {
  open: "text-fg",
  fixed: "text-up",
  accepted: "text-muted",
  ignored: "text-faint",
};

/** The expandable details of one finding, with accept/ignore/reopen. */
export function FindingDetail({ f }: { f: VulnFinding }) {
  const update = useUpdateVuln();
  const [note, setNote] = useState(f.note);
  return (
    <div className="grid gap-3 text-[13px] md:grid-cols-[minmax(0,1fr)_260px]">
      <div className="grid content-start gap-2.5">
        {f.description && <p className="leading-relaxed whitespace-pre-line text-muted">{f.description}</p>}
        {f.confidence === "low" && (
          <p className="rounded-md border border-amber-300/30 bg-amber-300/5 px-3 py-2 text-xs text-amber-200">
            Lower confidence: the version string looks like a distro package, and distros often backport security fixes without changing the version. Check your distro's security tracker
            before acting on it. Low-confidence findings never send alerts.
          </p>
        )}
        {f.evidence && (
          <div>
            <div className="mb-1 text-[11px] font-medium tracking-wide text-muted uppercase">Evidence</div>
            <pre className="max-h-48 overflow-auto rounded-md bg-black/30 px-3 py-2 font-mono text-[11px] whitespace-pre-wrap text-muted">{f.evidence}</pre>
          </div>
        )}
        {f.fixedIn && (
          <div className="text-xs">
            <span className="text-muted">Fixed in </span>
            <b className="font-mono">{f.fixedIn}</b>
            <span className="text-muted"> — update to this version or later.</span>
          </div>
        )}
        {f.references.length > 0 && (
          <div className="grid gap-0.5">
            <div className="text-[11px] font-medium tracking-wide text-muted uppercase">References</div>
            {f.references.slice(0, 8).map((r) => (
              <a key={r} href={r} target="_blank" rel="noreferrer noopener" className="flex items-center gap-1 truncate text-xs text-accent hover:underline">
                <ExternalLink size={11} className="shrink-0" /> <span className="truncate">{r}</span>
              </a>
            ))}
          </div>
        )}
      </div>
      <div className="grid content-start gap-2.5 text-xs">
        <div className="grid grid-cols-[90px_1fr] gap-y-1 text-muted">
          <span>Found by</span>
          <span className="text-fg">{sourceLabel[f.source]}</span>
          <span>Check</span>
          <span className="truncate font-mono text-fg" title={f.key}>
            {f.key}
          </span>
          {f.cvss > 0 && (
            <>
              <span>CVSS</span>
              <span className="text-fg">{f.cvss.toFixed(1)}</span>
            </>
          )}
          <span>First seen</span>
          <span className="text-fg" title={fmtDateTime(f.firstSeen)}>
            {ago(f.firstSeen)}
          </span>
          <span>Last seen</span>
          <span className="text-fg" title={fmtDateTime(f.lastSeen)}>
            {ago(f.lastSeen)}
          </span>
          <span>Status</span>
          <span className={clsx("capitalize", statusStyle[f.status])}>
            {f.status}
            {f.resolvedAt && f.status !== "open" ? ` ${ago(f.resolvedAt)}` : ""}
          </span>
        </div>
        <div>
          <label className="label">Note</label>
          <textarea
            className="input min-h-[52px] text-xs"
            value={note}
            placeholder="Why it's accepted, ticket link…"
            onChange={(e) => setNote(e.target.value)}
            onBlur={() => note !== f.note && update.mutate({ id: f.id, note })}
          />
        </div>
        <div className="flex flex-wrap gap-1.5">
          {f.status !== "open" && f.status !== "fixed" && (
            <button className="btn-outline py-1 text-xs" onClick={() => update.mutate({ id: f.id, status: "open" })}>
              <RotateCcw size={12} /> Reopen
            </button>
          )}
          {f.status !== "accepted" && f.status !== "fixed" && (
            <button className="btn-outline py-1 text-xs" title="You know about it and accept the risk; stays listed but out of the open count" onClick={() => update.mutate({ id: f.id, status: "accepted" })}>
              <Check size={12} /> Accept risk
            </button>
          )}
          {f.status !== "ignored" && f.status !== "fixed" && (
            <button className="btn-outline py-1 text-xs" title="False positive / not relevant" onClick={() => update.mutate({ id: f.id, status: "ignored" })}>
              <EyeOff size={12} /> False positive
            </button>
          )}
        </div>
        {f.status === "fixed" && <p className="text-faint">Marked fixed because the last scan didn't find it. It reopens automatically if it comes back.</p>}
      </div>
    </div>
  );
}

/** A compact, expandable list of findings (used on device / IP pages). */
export function FindingList({ findings, showDevice }: { findings: VulnFinding[]; showDevice?: boolean }) {
  const [open, setOpen] = useState<number | null>(null);
  return (
    <div className="overflow-hidden rounded-lg border border-line">
      {findings.map((f) => (
        <div key={f.id} className="border-b border-line/70 last:border-0">
          <button
            className={clsx("flex w-full items-center gap-2.5 px-3 py-2 text-left text-[13px] hover:bg-white/[0.03]", f.status !== "open" && "opacity-60")}
            onClick={() => setOpen(open === f.id ? null : f.id)}
          >
            <ChevronRight size={13} className={clsx("shrink-0 text-faint transition-transform", open === f.id && "rotate-90")} />
            <SeverityBadge severity={f.severity} className="w-[62px] text-center" />
            <span className="min-w-0 flex-1 truncate">
              {f.title}
              {f.kev && <span className="ml-2 rounded bg-red-600/20 px-1 text-[10px] font-semibold text-red-300">EXPLOITED</span>}
              {f.confidence === "low" && <span className="ml-2 text-[10px] text-faint">low confidence</span>}
            </span>
            {showDevice && <span className="shrink-0 text-xs text-muted">{f.deviceName || f.ip}</span>}
            <span className="shrink-0 font-mono text-xs text-faint">{f.port ? `:${f.port}` : "host"}</span>
            {f.status !== "open" && <span className="shrink-0 text-[10px] text-faint capitalize">{f.status}</span>}
          </button>
          {open === f.id && (
            <div className="border-t border-line/50 bg-panel/60 px-4 py-3">
              <FindingDetail f={f} />
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
