import { useMemo, useState } from "react";
import type { Bucket } from "@/lib/types";
import { fmtDateTime } from "@/lib/time";

const W = 720;
const H = 140;
const PAD = { l: 40, r: 8, t: 10, b: 20 };

/** Single-series latency area chart with a crosshair tooltip. */
export function LatencyChart({ buckets }: { buckets: Bucket[] }) {
  const [hover, setHover] = useState<number | null>(null);
  const pts = useMemo(() => buckets.map((b, i) => ({ i, v: b.latency, t: b.t })), [buckets]);
  const vals = pts.filter((p) => p.v != null).map((p) => p.v as number);
  if (vals.length < 2) return <div className="flex h-[140px] items-center justify-center text-xs text-muted">Not enough data for a latency chart yet.</div>;

  const max = niceMax(Math.max(...vals));
  const x = (i: number) => PAD.l + (i / Math.max(1, buckets.length - 1)) * (W - PAD.l - PAD.r);
  const y = (v: number) => PAD.t + (1 - v / max) * (H - PAD.t - PAD.b);

  // Break the line where buckets have no data.
  const segs: { i: number; v: number }[][] = [];
  let cur: { i: number; v: number }[] = [];
  for (const p of pts) {
    if (p.v == null) {
      if (cur.length) segs.push(cur);
      cur = [];
    } else cur.push({ i: p.i, v: p.v });
  }
  if (cur.length) segs.push(cur);

  const ticks = [0, max / 2, max];
  const h = hover !== null ? pts[hover] : null;

  return (
    <div className="relative">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="h-[140px] w-full"
        preserveAspectRatio="none"
        onMouseMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          const px = ((e.clientX - r.left) / r.width) * W;
          const i = Math.round(((px - PAD.l) / (W - PAD.l - PAD.r)) * (buckets.length - 1));
          setHover(Math.max(0, Math.min(buckets.length - 1, i)));
        }}
        onMouseLeave={() => setHover(null)}
        role="img"
        aria-label={`Response time, max ${Math.max(...vals)} ms`}
      >
        <defs>
          <linearGradient id="lat-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="var(--color-accent)" stopOpacity="0.28" />
            <stop offset="1" stopColor="var(--color-accent)" stopOpacity="0" />
          </linearGradient>
        </defs>
        {ticks.map((t) => (
          <g key={t}>
            <line x1={PAD.l} x2={W - PAD.r} y1={y(t)} y2={y(t)} stroke="var(--color-line)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
            <text x={PAD.l - 6} y={y(t) + 3} textAnchor="end" className="fill-faint" fontSize="10">
              {Math.round(t)}
            </text>
          </g>
        ))}
        {segs.map((s, k) => {
          const line = s.map((p, j) => `${j ? "L" : "M"}${x(p.i)},${y(p.v)}`).join(" ");
          const area = `${line} L${x(s[s.length - 1].i)},${y(0)} L${x(s[0].i)},${y(0)} Z`;
          return (
            <g key={k}>
              <path d={area} fill="url(#lat-fill)" />
              <path d={line} fill="none" stroke="var(--color-accent)" strokeWidth="2" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
            </g>
          );
        })}
        {h && (
          <line x1={x(h.i)} x2={x(h.i)} y1={PAD.t} y2={H - PAD.b} stroke="var(--color-muted)" strokeWidth="1" strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
        )}
        <text x={PAD.l} y={H - 4} className="fill-faint" fontSize="10">
          {fmtDateTime(buckets[0].t)}
        </text>
        <text x={W - PAD.r} y={H - 4} textAnchor="end" className="fill-faint" fontSize="10">
          now
        </text>
      </svg>
      {h && (
        <div
          className="pointer-events-none absolute top-0 -translate-x-1/2 rounded-md border border-line-2 bg-panel-2 px-2.5 py-1.5 text-xs shadow-xl"
          style={{ left: `${(x(h.i) / W) * 100}%` }}
        >
          <div className="text-muted">{fmtDateTime(h.t)}</div>
          <div className="font-medium tabular-nums">{h.v != null ? `${h.v} ms` : "no data"}</div>
        </div>
      )}
      <div className="absolute top-0 left-0 text-[10px] text-faint">ms</div>
    </div>
  );
}

function niceMax(v: number): number {
  if (v <= 0) return 10;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v * 1.1) return m * p;
  return 10 * p;
}
