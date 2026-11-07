import { useMemo, useRef, useState } from "react";
import clsx from "clsx";
import type { Bucket } from "@/lib/types";
import { fmtDateTime, pct } from "@/lib/time";

type Kind = "empty" | "up" | "degraded" | "down" | "maint";

function classify(b: Bucket): Kind {
  if (b.total === 0) return b.maint > 0 ? "maint" : "empty";
  if (b.up === b.total) return "up";
  if (b.up === 0) return "down";
  return "degraded";
}

// Degraded bars are shorter so they read apart from "up" without relying on
// green-vs-amber (which is weak under protanopia).
const style: Record<Kind, string> = {
  empty: "h-full bg-white/[0.06]",
  up: "h-full bg-up",
  degraded: "h-[55%] bg-warn",
  down: "h-full bg-down",
  maint: "h-full bg-maint/70",
};

const label: Record<Kind, string> = {
  empty: "No data",
  up: "Up",
  degraded: "Partial outage",
  down: "Down",
  maint: "Maintenance",
};

/** A row of uptime bars, one per time bucket, with a hover tooltip. */
export function UptimeBars({ buckets, bucketSeconds = 1800, height = 22, className }: { buckets: Bucket[]; bucketSeconds?: number; height?: number; className?: string }) {
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const kinds = useMemo(() => buckets.map(classify), [buckets]);
  const b = hover !== null ? buckets[hover] : null;

  return (
    <div className={clsx("relative", className)} onMouseLeave={() => setHover(null)}>
      <div ref={ref} className="flex items-end gap-[2px]" style={{ height }} role="img" aria-label={summary(buckets)}>
        {kinds.map((k, i) => (
          <div key={i} className="flex h-full min-w-[3px] flex-1 items-end" onMouseEnter={() => setHover(i)}>
            <div className={clsx("w-full rounded-[2px] transition-opacity", style[k], hover !== null && hover !== i && "opacity-60")} />
          </div>
        ))}
      </div>
      {b && hover !== null && (
        <div
          className="pointer-events-none absolute bottom-full z-20 mb-2 w-48 -translate-x-1/2 rounded-md border border-line-2 bg-panel-2 px-3 py-2 text-xs shadow-xl"
          style={{ left: `${((hover + 0.5) / buckets.length) * 100}%` }}
        >
          <div className="text-muted">
            {fmtDateTime(b.t)} – {fmtDateTime(new Date(Date.parse(b.t) + bucketSeconds * 1000).toISOString())}
          </div>
          <div className="mt-1 flex items-center justify-between font-medium">
            <span>{label[kinds[hover]]}</span>
            {b.total > 0 && <span className="tabular-nums">{pct(b.up / b.total, 1)}</span>}
          </div>
          {b.total > 0 && (
            <div className="mt-0.5 text-muted tabular-nums">
              {b.up}/{b.total} checks ok{b.latency != null ? ` · ${b.latency} ms avg` : ""}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function summary(buckets: Bucket[]): string {
  const up = buckets.reduce((a, b) => a + b.up, 0);
  const tot = buckets.reduce((a, b) => a + b.total, 0);
  return tot ? `Uptime ${pct(up / tot)} over ${buckets.length} periods` : "No data yet";
}
