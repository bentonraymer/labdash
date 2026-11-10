import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { AlertTriangle, Check, Database, Globe, Loader2, Play, RefreshCw, Search, ShieldCheck } from "lucide-react";
import { useRunVulnScan, useScanTargets, useVulns, useVulnStatus, useVulnSync } from "@/lib/queries";
import type { ScanTarget, Severity, VulnFinding, VulnScanState, VulnStatusKind } from "@/lib/types";
import { ago, duration, fmtDateTime } from "@/lib/time";
import { FindingList, sevStyle, severities, sourceLabel } from "@/components/VulnBits";
import { PageHeader } from "../AdminLayout";

const statusTabs: { id: VulnStatusKind | "all"; label: string }[] = [
  { id: "open", label: "Open" },
  { id: "accepted", label: "Accepted" },
  { id: "ignored", label: "False positives" },
  { id: "fixed", label: "Fixed" },
  { id: "all", label: "All" },
];

/** Every vulnerability finding across the network, with data-source health. */
export function VulnerabilitiesPage() {
  const [status, setStatus] = useState<VulnStatusKind | "all">("open");
  const [sev, setSev] = useState<Severity | "kev" | null>(null);
  const [source, setSource] = useState<VulnFinding["source"] | "">("");
  const [q, setQ] = useState("");
  const { data: findings = [], isLoading } = useVulns(status);
  const { data: st } = useVulnStatus();
  const { data: targets = [] } = useScanTargets();
  const run = useRunVulnScan();
  const [target, setTarget] = useState<number | "">("");

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return findings.filter(
      (f) =>
        (!sev || (sev === "kev" ? f.kev : f.severity === sev)) &&
        (!source || f.source === source) &&
        (!needle || `${f.title} ${f.key} ${f.ip} ${f.deviceName} ${f.port}`.toLowerCase().includes(needle)),
    );
  }, [findings, sev, source, q]);

  const scan = st?.scan;
  const open = st?.open ?? {};
  const pick = target || targets[0]?.id;
  const lastScan = st?.recent?.[0];

  return (
    <>
      <PageHeader
        title="Vulnerabilities"
        subtitle="Known CVEs by service version, web checks and weak configuration. Safe checks only: no exploits, brute force or DoS."
        actions={
          targets.length > 0 && (
            <div className="flex items-center gap-1.5">
              <select className="input w-auto py-1.5 text-xs" value={pick ?? ""} onChange={(e) => setTarget(Number(e.target.value))}>
                {targets.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name || t.cidr} ({t.cidr})
                  </option>
                ))}
              </select>
              <button className="btn-primary py-1.5" disabled={!pick || scan?.running || run.isPending} onClick={() => pick && run.mutate(pick)}>
                {scan?.running ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />} Scan now
              </button>
            </div>
          )
        }
      />
      <div className="grid gap-5 p-6">
        {scan?.running && <ScanProgress scan={scan} targets={targets} />}
        {run.error && <div className="text-sm text-down">{run.error.message}</div>}

        <DataSources />

        {/* Severity tiles (open findings) */}
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
          {(["critical", "high", "medium", "low"] as Severity[]).map((s) => (
            <button
              key={s}
              onClick={() => {
                setStatus("open");
                setSev(sev === s ? null : s);
              }}
              className={clsx("rounded-lg border bg-panel px-4 py-3 text-left transition", sev === s ? "border-accent" : "border-line hover:border-line-2")}
            >
              <div className={clsx("text-2xl font-semibold tabular-nums", (open[s] ?? 0) > 0 ? sevStyle[s].text : "text-faint")}>{open[s] ?? 0}</div>
              <div className="text-xs text-muted capitalize">{s}</div>
            </button>
          ))}
          <button
            onClick={() => {
              setStatus("open");
              setSev(sev === "kev" ? null : "kev");
            }}
            className={clsx("rounded-lg border bg-panel px-4 py-3 text-left transition", sev === "kev" ? "border-accent" : "border-line hover:border-line-2")}
            title="On CISA's Known Exploited Vulnerabilities list: attackers are actively using these"
          >
            <div className={clsx("text-2xl font-semibold tabular-nums", (st?.kev ?? 0) > 0 ? "text-red-400" : "text-faint")}>{st?.kev ?? 0}</div>
            <div className="text-xs text-muted">Exploited in the wild</div>
          </button>
          <div className="rounded-lg border border-line bg-panel px-4 py-3">
            <div className="text-sm font-medium">{lastScan ? ago(lastScan.finishedAt ?? lastScan.startedAt) : "Never"}</div>
            <div className="text-xs text-muted">Last scan{lastScan ? ` · ${lastScan.devices} devices` : ""}</div>
          </div>
        </div>

        {lastScan?.notes && (
          <div className="grid gap-1 rounded-lg border border-warn/30 bg-warn/5 px-4 py-2.5 text-xs text-warn">
            {lastScan.notes.split("\n").map((n) => (
              <div key={n} className="flex items-start gap-2">
                <AlertTriangle size={13} className="mt-px shrink-0" /> {n}
              </div>
            ))}
          </div>
        )}
        {lastScan?.status === "failed" && <div className="text-sm text-down">Last scan failed: {lastScan.error}</div>}

        {/* Filters */}
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex rounded-md border border-line p-0.5">
            {statusTabs.map((t) => (
              <button key={t.id} onClick={() => setStatus(t.id)} className={clsx("rounded px-2.5 py-1 text-xs", status === t.id ? "bg-white/10 text-fg" : "text-muted hover:text-fg")}>
                {t.label}
              </button>
            ))}
          </div>
          <select className="input w-auto py-1 text-xs" value={sev ?? ""} onChange={(e) => setSev((e.target.value || null) as Severity | "kev" | null)}>
            <option value="">Any severity</option>
            {severities.map((s) => (
              <option key={s} value={s}>
                {s[0].toUpperCase() + s.slice(1)}
              </option>
            ))}
            <option value="kev">Exploited in the wild</option>
          </select>
          <select className="input w-auto py-1 text-xs" value={source} onChange={(e) => setSource(e.target.value as VulnFinding["source"] | "")}>
            <option value="">All checks</option>
            {(Object.keys(sourceLabel) as VulnFinding["source"][]).map((s) => (
              <option key={s} value={s}>
                {sourceLabel[s]}
              </option>
            ))}
          </select>
          <div className="relative ml-auto">
            <Search size={13} className="absolute top-1/2 left-2.5 -translate-y-1/2 text-faint" />
            <input className="input w-64 py-1 pl-8 text-xs" placeholder="Filter by CVE, device, IP, port…" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
        </div>

        {!isLoading && rows.length === 0 ? (
          <div className="flex flex-col items-center gap-2 rounded-lg border border-line py-12 text-center text-sm text-muted">
            <ShieldCheck size={28} className="text-up" />
            {findings.length === 0 && status === "open"
              ? lastScan
                ? "No open findings. Nice."
                : targets.length === 0
                  ? "Add a scan target on the Devices page first, then run a scan here."
                  : "No vulnerability scans yet. They run weekly, or start one with Scan now."
              : "Nothing matches these filters."}
          </div>
        ) : (
          <FindingList findings={rows} showDevice />
        )}

        {(st?.recent?.length ?? 0) > 0 && (
          <div>
            <div className="mb-1.5 text-[11px] font-medium tracking-wide text-muted uppercase">Recent scans</div>
            <div className="grid gap-1 text-xs">
              {st!.recent!.map((r) => (
                <div key={r.id} className="flex gap-3">
                  <span className="w-36 shrink-0 text-faint">{fmtDateTime(r.startedAt)}</span>
                  <span className="w-32 font-mono">{r.cidr}</span>
                  <span className={r.status === "failed" ? "text-down" : "text-muted"}>
                    {r.status === "running" ? "running…" : r.status === "failed" ? `failed: ${r.error}` : `${r.devices} devices · ${r.newCount} new · ${r.openCount} open`}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
        <p className="text-xs text-faint">
          Scans run on each target's weekly vulnerability schedule (set it in <Link to="/network/devices" className="text-accent hover:underline">Devices → Scan targets</Link>). New High or Critical
          findings send a Pushover alert; change the threshold in <Link to="/settings" className="text-accent hover:underline">Settings</Link>.
        </p>
      </div>
    </>
  );
}

const stepNames = ["Config & TLS checks", "CVE matching", "Web checks"];
const stepHints = [
  "nmap checks versions, TLS and risky settings on each open port",
  "Detected versions are looked up in the local CVE database",
  "Nuclei runs safe web checks against each web port",
];

/** Live progress of a running vulnerability scan: steps, bar, time, ETA. */
function ScanProgress({ scan, targets }: { scan: VulnScanState; targets: ScanTarget[] }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const step = scan.step ?? 0;
  const steps = scan.steps ?? stepNames.length;
  const pct = Math.max(0, Math.min(100, scan.percent ?? 0));
  const known = (scan.percent ?? -1) >= 0;
  const elapsed = scan.started ? now - Date.parse(scan.started) : 0;
  const stepElapsed = scan.stepStarted ? now - Date.parse(scan.stepStarted) : 0;
  // Remaining time for this step from its pace so far (once there's enough to go on).
  const eta = known && pct >= 3 && pct < 100 && stepElapsed > 10_000 ? (stepElapsed * (100 - pct)) / pct : null;
  const name = (id: number) => targets.find((t) => t.id === id)?.name;
  const queued = scan.queued.filter((id) => id !== scan.targetId);
  return (
    <div className="grid gap-3 rounded-lg border border-accent/40 bg-accent/[0.07] px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        <Loader2 size={15} className="animate-spin text-accent" />
        <span>
          Scanning <b>{name(scan.targetId) || "target"}</b> <span className="font-mono text-muted">{scan.cidr}</span>
        </span>
        <span className="text-xs text-muted">
          {scan.devices ? `${scan.devices} device${scan.devices === 1 ? "" : "s"}` : ""}
          {scan.urls ? ` · ${scan.urls} web URL${scan.urls === 1 ? "" : "s"}` : ""}
        </span>
        <span className="ml-auto text-xs text-muted tabular-nums">
          {(scan.found ?? 0) > 0 && <b className="mr-3 text-warn">{scan.found} found so far</b>}
          {duration(elapsed)} elapsed
        </span>
      </div>

      <div className="grid grid-cols-3 gap-2">
        {stepNames.slice(0, steps).map((label, i) => {
          const n = i + 1;
          const state = n < step ? "done" : n === step ? "active" : "pending";
          return (
            <div
              key={label}
              title={stepHints[i]}
              className={clsx(
                "flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs",
                state === "done" && "border-up/30 text-up",
                state === "active" && "border-accent/60 bg-accent/10 text-fg",
                state === "pending" && "border-line text-faint",
              )}
            >
              <span
                className={clsx(
                  "grid size-4 shrink-0 place-items-center rounded-full text-[10px] font-semibold",
                  state === "done" && "bg-up/20",
                  state === "active" && "bg-accent text-white",
                  state === "pending" && "border border-line-2",
                )}
              >
                {state === "done" ? <Check size={10} /> : n}
              </span>
              <span className="truncate">{label}</span>
              {state === "active" && known && <span className="ml-auto tabular-nums text-muted">{Math.floor(pct)}%</span>}
            </div>
          );
        })}
      </div>

      <div>
        <div className="h-1.5 overflow-hidden rounded-full bg-white/10">
          {known ? (
            <div className="h-full rounded-full bg-accent transition-[width] duration-700" style={{ width: `${pct}%` }} />
          ) : (
            <div className="h-full w-1/3 animate-pulse rounded-full bg-accent/60" />
          )}
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-3 text-xs text-muted">
          <span className="min-w-0 truncate">{scan.detail || scan.phase || "Starting…"}</span>
          <span className="ml-auto tabular-nums">
            {eta !== null ? `about ${duration(eta)} left in this step` : step > 0 ? `step ${step} of ${steps}` : ""}
          </span>
        </div>
      </div>
      {queued.length > 0 && <div className="text-xs text-faint">Queued next: {queued.map((id) => name(id) || `#${id}`).join(", ")}</div>}
    </div>
  );
}

/** Health of the local CVE mirror and Nuclei templates. */
export function DataSources({ compact }: { compact?: boolean }) {
  const { data: st } = useVulnStatus();
  const sync = useVulnSync();
  const db = st?.cveDb;
  const nuc = st?.nuclei;
  if (!st) return null;
  return (
    <div className={clsx("grid gap-3", !compact && "md:grid-cols-2")}>
      <div className="flex items-start gap-3 rounded-lg border border-line bg-panel px-4 py-3 text-xs">
        <Database size={16} className="mt-0.5 shrink-0 text-muted" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">CVE database</div>
          {db?.syncing ? (
            <div className="flex items-center gap-1.5 text-accent">
              <Loader2 size={12} className="animate-spin" /> {db.progress || "Syncing…"}
            </div>
          ) : db && db.cves > 0 ? (
            <div className="text-muted">
              {db.cves.toLocaleString()} CVEs · {db.kev.toLocaleString()} known exploited · updated {ago(db.lastSync)}
            </div>
          ) : (
            <div className="text-warn">Not downloaded yet (about 115 MB from the NVD mirror, then small daily updates).</div>
          )}
          {db?.error && !db.syncing && <div className="mt-0.5 text-down">{db.error}</div>}
        </div>
        <button className="btn-outline shrink-0 py-1 text-xs" disabled={db?.syncing || sync.isPending} onClick={() => sync.mutate({ what: "cve" })}>
          <RefreshCw size={12} /> {db && db.cves > 0 ? "Update" : "Download"}
        </button>
      </div>
      <div className="flex items-start gap-3 rounded-lg border border-line bg-panel px-4 py-3 text-xs">
        <Globe size={16} className="mt-0.5 shrink-0 text-muted" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">Web checks (Nuclei{nuc?.version ? ` ${nuc.version}` : ""})</div>
          {!nuc?.installed ? (
            <div className="text-warn">Nuclei isn't installed in this container, so web checks are skipped.</div>
          ) : nuc.templatesUpdated ? (
            <div className="text-muted">Templates updated {ago(nuc.templatesUpdated)}</div>
          ) : (
            <div className="text-warn">Templates not downloaded yet (they update daily).</div>
          )}
          {nuc?.error && <div className="mt-0.5 text-down">{nuc.error}</div>}
        </div>
        {nuc?.installed && (
          <button className="btn-outline shrink-0 py-1 text-xs" disabled={sync.isPending} onClick={() => sync.mutate({ what: "templates" })}>
            <RefreshCw size={12} /> Update
          </button>
        )}
      </div>
      {sync.error && <div className="text-xs text-down">{sync.error.message}</div>}
    </div>
  );
}
