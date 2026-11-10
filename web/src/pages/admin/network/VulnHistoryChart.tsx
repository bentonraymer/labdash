import { useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { useVulnHistory } from "@/lib/queries";
import type { VulnHistoryPoint } from "@/lib/types";

// One-hue ordinal ramp (severity is ordered), validated for the dark
// surface: monotone lightness, visible steps, every step >= 3:1 contrast.
const series = [
  { key: "critical", label: "Critical", color: "#c81e3c" },
  { key: "high", label: "High", color: "#f2546a" },
  { key: "medium", label: "Medium", color: "#fca5ad" },
  { key: "low", label: "Low", color: "#fde2e4" },
] as const;
type SevKey = (typeof series)[number]["key"];

const ranges = [
  { days: 30, label: "30d" },
  { days: 90, label: "90d" },
  { days: 365, label: "1y" },
];

const H = 180; // plot height
const PAD = { top: 8, right: 8, bottom: 22, left: 34 };

const total = (p: VulnHistoryPoint) => p.critical + p.high + p.medium + p.low;
const fmtDay = (d: string, long = false) =>
  new Date(`${d}T12:00:00`).toLocaleDateString(undefined, long ? { weekday: "short", month: "short", day: "numeric", year: "numeric" } : { month: "short", day: "numeric" });

/** Clean y-axis ticks: 0 plus up to 3 round steps covering max. */
function ticks(max: number): number[] {
  if (max <= 0) return [0, 1];
  const raw = max / 3;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? 10 * mag;
  const s = Math.max(1, Math.round(step));
  const out: number[] = [];
  for (let v = 0; v <= max + s - 1 && out.length < 6; v += s) out.push(v);
  if (out[out.length - 1] < max) out.push(out[out.length - 1] + s);
  return out;
}

/** Open findings per day, stacked by severity. */
export function VulnHistoryChart() {
  const [days, setDays] = useState(90);
  const [view, setView] = useState<"chart" | "table">("chart");
  const { data = [], isLoading } = useVulnHistory(days);
  const wrap = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(800);
  const [hover, setHover] = useState<number | null>(null);

  const empty = !isLoading && data.every((p) => total(p) === 0);
  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.max(280, Math.floor(e.contentRect.width))));
    ro.observe(el);
    return () => ro.disconnect();
  }, [view, empty]);

  const max = useMemo(() => Math.max(0, ...data.map(total)), [data]);
  const yTicks = useMemo(() => ticks(max), [max]);
  const top = yTicks[yTicks.length - 1] || 1;
  const anyEstimated = data.some((p) => p.estimated && total(p) > 0);
  const latest = data[data.length - 1];
  const first = data.find((p) => total(p) > 0) ?? data[0];

  const plotW = width - PAD.left - PAD.right;
  const slot = data.length ? plotW / data.length : plotW;
  const gap = slot >= 6 ? 2 : slot >= 3 ? 1 : 0;
  const barW = Math.max(1, slot - gap);
  const y = (v: number) => PAD.top + H - (v / top) * H;

  // A few x labels that don't collide.
  const xLabels = useMemo(() => {
    if (data.length === 0) return [] as number[];
    const every = Math.max(1, Math.ceil(data.length / Math.max(2, Math.floor(plotW / 70))));
    const idx: number[] = [];
    for (let i = data.length - 1; i >= 0; i -= every) idx.push(i);
    return idx;
  }, [data.length, plotW]);

  const hp = hover !== null ? data[hover] : null;

  return (
    <div className="min-w-0 rounded-lg border border-line bg-panel">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-line px-4 py-2.5">
        <div>
          <div className="text-sm font-semibold">Open vulnerabilities over time</div>
          {latest && first && !empty && (
            <div className="text-xs text-muted">
              {total(latest)} open now
              {total(first) !== total(latest) && (
                <>
                  {" "}
                  · {total(latest) < total(first) ? "down" : "up"} {Math.abs(total(latest) - total(first))} since {fmtDay(first.day)}
                </>
              )}
            </div>
          )}
        </div>
        <div className="flex items-center gap-3 text-xs text-muted" aria-label="Legend">
          {series.map((s) => (
            <span key={s.key} className="flex items-center gap-1.5">
              <span className="size-2.5 rounded-sm" style={{ background: s.color }} />
              {s.label}
            </span>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-2">
          <div className="flex rounded-md border border-line p-0.5">
            {ranges.map((r) => (
              <button key={r.days} onClick={() => setDays(r.days)} className={clsx("rounded px-2 py-0.5 text-xs", days === r.days ? "bg-white/10 text-fg" : "text-muted hover:text-fg")}>
                {r.label}
              </button>
            ))}
          </div>
          <div className="flex rounded-md border border-line p-0.5">
            {(["chart", "table"] as const).map((v) => (
              <button key={v} onClick={() => setView(v)} className={clsx("rounded px-2 py-0.5 text-xs capitalize", view === v ? "bg-white/10 text-fg" : "text-muted hover:text-fg")}>
                {v}
              </button>
            ))}
          </div>
        </div>
      </div>

      {empty ? (
        <p className="px-4 py-8 text-center text-sm text-muted">No vulnerability history yet. It builds up as scans run.</p>
      ) : view === "table" ? (
        <HistoryTable data={data} />
      ) : (
        <div className="relative min-w-0 overflow-hidden px-2 pt-2 pb-1" onPointerLeave={() => setHover(null)}>
          {/* Measured separately so the SVG can't hold its container open. */}
          <div ref={wrap} className="h-0 w-full" />
          <svg className="block" width={width} height={H + PAD.top + PAD.bottom} role="img" aria-label={`Open vulnerabilities per day over the last ${days} days, stacked by severity`}>
            {/* gridlines + y ticks */}
            {yTicks.map((t) => (
              <g key={t}>
                <line x1={PAD.left} x2={width - PAD.right} y1={y(t)} y2={y(t)} stroke="var(--color-line)" strokeWidth={1} />
                <text x={PAD.left - 6} y={y(t)} dy="0.32em" textAnchor="end" className="fill-[var(--color-faint)] text-[10px] tabular-nums">
                  {t}
                </text>
              </g>
            ))}
            {/* bars */}
            {data.map((p, i) => {
              const x = PAD.left + i * slot + gap / 2;
              let base = 0;
              const segs: { key: SevKey; y0: number; y1: number; color: string }[] = [];
              for (const s of series) {
                const v = p[s.key];
                if (v > 0) {
                  segs.push({ key: s.key, y0: base, y1: base + v, color: s.color });
                  base += v;
                }
              }
              const r = Math.min(4, barW / 2);
              return (
                <g key={p.day} opacity={p.estimated ? 0.45 : 1} className={clsx(hover === i && "brightness-125")}>
                  {segs.map((sg, j) => {
                    const isTop = j === segs.length - 1;
                    const yTop = y(sg.y1);
                    // 2px surface gap between stacked segments (skip when too short).
                    let h = y(sg.y0) - yTop - (j > 0 ? 2 : 0);
                    if (h < 0.5) h = Math.max(0.5, y(sg.y0) - yTop);
                    const yy = yTop;
                    if (!isTop || r < 1) return <rect key={sg.key} x={x} y={yy} width={barW} height={h} fill={sg.color} />;
                    // Rounded data-end on top only; square at the baseline side.
                    const rr = Math.min(r, h);
                    return (
                      <path
                        key={sg.key}
                        fill={sg.color}
                        d={`M${x},${yy + h} V${yy + rr} Q${x},${yy} ${x + rr},${yy} H${x + barW - rr} Q${x + barW},${yy} ${x + barW},${yy + rr} V${yy + h} Z`}
                      />
                    );
                  })}
                </g>
              );
            })}
            {/* x labels */}
            {xLabels.map((i) => {
              const cx = PAD.left + i * slot + slot / 2;
              // Keep edge labels inside the plot instead of clipping them.
              const anchor = cx > width - PAD.right - 24 ? "end" : cx < PAD.left + 24 ? "start" : "middle";
              const lx = anchor === "end" ? width - PAD.right : anchor === "start" ? PAD.left : cx;
              return (
                <text key={i} x={lx} y={PAD.top + H + 15} textAnchor={anchor} className="fill-[var(--color-faint)] text-[10px]">
                  {fmtDay(data[i].day)}
                </text>
              );
            })}
            {/* hover targets: the whole column */}
            {data.map((p, i) => (
              <rect
                key={`hit-${p.day}`}
                x={PAD.left + i * slot}
                y={PAD.top}
                width={slot}
                height={H}
                fill="transparent"
                tabIndex={-1}
                onPointerEnter={() => setHover(i)}
                onPointerMove={() => setHover(i)}
              />
            ))}
            {hover !== null && <line x1={PAD.left + hover * slot + slot / 2} x2={PAD.left + hover * slot + slot / 2} y1={PAD.top} y2={PAD.top + H} stroke="var(--color-line-2)" strokeWidth={1} pointerEvents="none" />}
          </svg>
          {hp && (
            <div
              className="pointer-events-none absolute top-2 z-10 w-48 rounded-md border border-line-2 bg-panel-2 px-3 py-2 text-xs shadow-lg"
              style={PAD.left + hover! * slot + slot / 2 > width / 2 ? { right: width - (PAD.left + hover! * slot) + 12 } : { left: PAD.left + (hover! + 1) * slot + 16 }}
            >
              <div className="mb-1 font-medium">{fmtDay(hp.day, true)}</div>
              {series.map((s) => (
                <div key={s.key} className="flex items-center gap-2">
                  <span className="size-2 rounded-sm" style={{ background: s.color }} />
                  <span className="text-muted">{s.label}</span>
                  <span className="ml-auto tabular-nums">{hp[s.key]}</span>
                </div>
              ))}
              <div className="mt-1 flex border-t border-line pt-1">
                <span className="text-muted">Total open</span>
                <span className="ml-auto font-medium tabular-nums">{total(hp)}</span>
              </div>
              {hp.kev > 0 && (
                <div className="flex">
                  <span className="text-muted">Exploited in the wild</span>
                  <span className="ml-auto tabular-nums">{hp.kev}</span>
                </div>
              )}
              {hp.info > 0 && (
                <div className="flex text-faint">
                  <span>Info (not charted)</span>
                  <span className="ml-auto tabular-nums">{hp.info}</span>
                </div>
              )}
              {hp.estimated && <div className="mt-1 text-[11px] text-faint">Estimated from when findings were first seen and resolved.</div>}
            </div>
          )}
          {anyEstimated && <p className="px-2 pb-1 text-[11px] text-faint">Faded days are estimates from finding dates, from before daily history was recorded.</p>}
        </div>
      )}
    </div>
  );
}

function HistoryTable({ data }: { data: VulnHistoryPoint[] }) {
  // Only days where something changed, newest first.
  const rows = data.filter((p, i) => i === data.length - 1 || i === 0 || series.some((s) => p[s.key] !== data[i - 1][s.key]) || p.kev !== data[i - 1].kev).reverse();
  return (
    <div className="max-h-72 overflow-auto">
      <table className="w-full text-xs">
        <thead className="sticky top-0 bg-panel text-muted">
          <tr className="border-b border-line">
            <th className="px-4 py-1.5 text-left font-medium">Day</th>
            {series.map((s) => (
              <th key={s.key} className="px-3 py-1.5 text-right font-medium">
                {s.label}
              </th>
            ))}
            <th className="px-3 py-1.5 text-right font-medium">Total</th>
            <th className="px-4 py-1.5 text-right font-medium">Exploited</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((p) => (
            <tr key={p.day} className={clsx("border-b border-line/50 last:border-0", p.estimated && "text-muted")}>
              <td className="px-4 py-1">
                {fmtDay(p.day, true)}
                {p.estimated && <span className="ml-1.5 text-[10px] text-faint">estimated</span>}
              </td>
              {series.map((s) => (
                <td key={s.key} className="px-3 py-1 text-right tabular-nums">
                  {p[s.key]}
                </td>
              ))}
              <td className="px-3 py-1 text-right font-medium tabular-nums">{total(p)}</td>
              <td className="px-4 py-1 text-right tabular-nums">{p.kev}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="px-4 py-1.5 text-[11px] text-faint">Showing the days the counts changed.</p>
    </div>
  );
}
