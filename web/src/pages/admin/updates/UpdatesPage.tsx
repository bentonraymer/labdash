import { useMemo, useState } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { AlertTriangle, ArrowUpCircle, Box, CheckCircle2, ChevronRight, CircleHelp, ExternalLink, GitBranch, Loader2, RefreshCw, Search, Server, Settings2, SkipForward, Wrench } from "lucide-react";
import { useCheckUpdates, useSaveUpdateConfig, useUpdates } from "@/lib/queries";
import type { NodeUpdates, UpdateItem, UpdateStatus } from "@/lib/types";
import { ago, fmtDateTime } from "@/lib/time";
import { ServiceIcon } from "@/components/ServiceIcon";
import { MarkdownPreview } from "@/components/MarkdownPreview";
import { PageHeader } from "../AdminLayout";
import { UpdateConfigModal } from "./UpdateConfigModal";

const statusInfo: Record<UpdateStatus, { label: string; cls: string }> = {
  outdated: { label: "Update available", cls: "bg-accent/15 text-accent" },
  current: { label: "Up to date", cls: "bg-up/15 text-up" },
  unknown: { label: "Unknown", cls: "bg-white/10 text-muted" },
  error: { label: "Check failed", cls: "bg-down/15 text-down" },
  untracked: { label: "Not tracked", cls: "bg-white/5 text-faint" },
};

export const sourceInfo: Record<string, { label: string; icon: typeof Box }> = {
  docker: { label: "Docker", icon: Box },
  github: { label: "GitHub", icon: GitBranch },
  helper: { label: "Helper Script", icon: Wrench },
  none: { label: "Not tracked", icon: CircleHelp },
  "": { label: "—", icon: CircleHelp },
};

type Filter = "all" | "outdated" | "current" | "attention" | "untracked";

/** Which services have newer versions, plus pending Proxmox package updates. Read-only. */
export function UpdatesPage() {
  const { data, isLoading } = useUpdates();
  const check = useCheckUpdates();
  const [filter, setFilter] = useState<Filter>("all");
  const [q, setQ] = useState("");
  const [open, setOpen] = useState<number | null>(null);
  const [editing, setEditing] = useState<UpdateItem | null>(null);
  const items = useMemo(() => data?.items ?? [], [data]);
  const checking = !!data?.state?.checking || check.isPending;

  const counts = useMemo(() => {
    const c = { outdated: 0, current: 0, attention: 0, untracked: 0 };
    for (const it of items) {
      if (it.status === "outdated") c.outdated++;
      else if (it.status === "current") c.current++;
      else if (it.status === "untracked") c.untracked++;
      else c.attention++;
    }
    return c;
  }, [items]);

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const rank: Record<UpdateStatus, number> = { outdated: 0, error: 1, unknown: 2, current: 3, untracked: 4 };
    return items
      .filter((it) => {
        if (filter === "outdated" && it.status !== "outdated") return false;
        if (filter === "current" && it.status !== "current") return false;
        if (filter === "untracked" && it.status !== "untracked") return false;
        if (filter === "attention" && it.status !== "error" && it.status !== "unknown") return false;
        return !needle || `${it.name} ${it.image} ${it.resolvedRepo} ${it.githubRepo}`.toLowerCase().includes(needle);
      })
      .sort((a, b) => rank[a.status] - rank[b.status] || a.name.localeCompare(b.name));
  }, [items, filter, q]);

  const nodes = data?.state?.nodes ?? [];
  const noSources = !isLoading && (data?.endpoints.length ?? 0) === 0 && items.every((i) => i.status === "untracked");

  return (
    <>
      <PageHeader
        title="Updates"
        subtitle="Newer versions for your services and Proxmox nodes. LabDash only reports; you update with your usual tools."
        actions={
          <button className="btn-primary py-1.5" disabled={checking} onClick={() => check.mutate(undefined)}>
            {checking ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />} {checking ? "Checking…" : "Check now"}
          </button>
        }
      />
      <div className="grid gap-5 p-6">
        {check.error && <div className="text-sm text-down">{check.error.message}</div>}
        {noSources && (
          <div className="grid gap-1 rounded-lg border border-accent/40 bg-accent/5 px-4 py-3 text-sm">
            <b>Get started</b>
            <span className="text-muted">
              Connect your Docker hosts in <Link to="/settings#docker-hosts" className="text-accent hover:underline">Settings → Docker hosts</Link> so containers are checked automatically.
              For Proxmox Helper Script LXCs and other apps, use <b>Configure</b> on a service below.
            </span>
          </div>
        )}

        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <Tile n={counts.outdated} label="Updates available" tone="text-accent" active={filter === "outdated"} onClick={() => setFilter(filter === "outdated" ? "all" : "outdated")} />
          <Tile n={counts.current} label="Up to date" tone="text-up" active={filter === "current"} onClick={() => setFilter(filter === "current" ? "all" : "current")} />
          <Tile n={counts.attention} label="Need attention" tone="text-warn" active={filter === "attention"} onClick={() => setFilter(filter === "attention" ? "all" : "attention")} />
          <Tile n={counts.untracked} label="Not tracked" tone="text-muted" active={filter === "untracked"} onClick={() => setFilter(filter === "untracked" ? "all" : "untracked")} />
        </div>

        {nodes.length > 0 && <ProxmoxNodes nodes={nodes} at={data?.state?.nodesAt} />}

        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted">
            {data?.state?.lastRun ? `Last checked ${ago(data.state.lastRun)}` : "Not checked yet"} · checks run every 6 hours
          </span>
          <div className="relative ml-auto">
            <Search size={13} className="absolute top-1/2 left-2.5 -translate-y-1/2 text-faint" />
            <input className="input w-64 py-1 pl-8 text-xs" placeholder="Filter services, images, repos…" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
        </div>

        <div className="overflow-x-auto rounded-lg border border-line">
          <table className="w-full text-[13px]">
            <thead className="border-b border-line bg-panel text-xs text-muted">
              <tr>
                <th className="w-6" />
                <th className="px-3 py-2 text-left font-medium">Service</th>
                <th className="px-3 py-2 text-left font-medium">Source</th>
                <th className="px-3 py-2 text-left font-medium">Running</th>
                <th className="px-3 py-2 text-left font-medium">Latest</th>
                <th className="px-3 py-2 text-left font-medium">Status</th>
                <th className="px-3 py-2 text-left font-medium">Checked</th>
                <th className="w-10 xl:w-24" />
              </tr>
            </thead>
            <tbody>
              {!isLoading && rows.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-3 py-10 text-center text-muted">
                    {items.length === 0 ? "No services yet." : "Nothing matches."}
                  </td>
                </tr>
              )}
              {rows.map((it) => (
                <UpdateRow key={it.serviceId} it={it} open={open === it.serviceId} onToggle={() => setOpen(open === it.serviceId ? null : it.serviceId)} onConfigure={() => setEditing(it)} />
              ))}
            </tbody>
          </table>
        </div>
      </div>
      {editing && <UpdateConfigModal item={editing} endpoints={data?.endpoints ?? []} hasGithubToken={!!data?.hasGithubToken} onClose={() => setEditing(null)} />}
    </>
  );
}

function Tile({ n, label, tone, active, onClick }: { n: number; label: string; tone: string; active: boolean; onClick: () => void }) {
  return (
    <button onClick={onClick} className={clsx("rounded-lg border bg-panel px-4 py-3 text-left transition", active ? "border-accent" : "border-line hover:border-line-2")}>
      <div className={clsx("text-2xl font-semibold tabular-nums", n > 0 ? tone : "text-faint")}>{n}</div>
      <div className="text-xs text-muted">{label}</div>
    </button>
  );
}

function UpdateRow({ it, open, onToggle, onConfigure }: { it: UpdateItem; open: boolean; onToggle: () => void; onConfigure: () => void }) {
  const save = useSaveUpdateConfig();
  const check = useCheckUpdates();
  const st = statusInfo[it.status];
  const src = sourceInfo[it.usedSource] ?? sourceInfo[""];
  const SrcIcon = src.icon;
  const skip = () =>
    save.mutate({
      serviceId: it.serviceId,
      source: it.source,
      dockerEndpointId: it.dockerEndpointId,
      container: it.container,
      githubRepo: it.githubRepo,
      helperSlug: it.helperSlug,
      currentVersion: it.currentVersion,
      ignoredVersion: it.newDigest && it.latestVersion.endsWith("(newer image)") ? "" : it.latestVersion,
    });
  return (
    <>
      <tr onClick={onToggle} className={clsx("cursor-pointer border-b border-line/70 hover:bg-white/[0.03]", open && "bg-white/[0.03]", it.status === "untracked" && "opacity-60")}>
        <td className="pl-3">
          <ChevronRight size={14} className={clsx("text-faint transition-transform", open && "rotate-90")} />
        </td>
        <td className="px-3 py-2">
          <div className="flex items-center gap-2">
            <ServiceIcon icon={it.icon} name={it.name} className="size-5 shrink-0" />
            <span className="font-medium">{it.name}</span>
          </div>
        </td>
        <td className="px-3 py-2 whitespace-nowrap text-muted">
          <span className="flex items-center gap-1.5">
            <SrcIcon size={13} />
            {src.label}
          </span>
        </td>
        <td className="max-w-[180px] truncate px-3 py-2 font-mono text-xs" title={it.runningVersion}>
          {it.runningVersion || <span className="text-faint">—</span>}
        </td>
        <td className="max-w-[200px] truncate px-3 py-2 font-mono text-xs" title={it.latestVersion}>
          {it.latestVersion ? <span className={it.status === "outdated" ? "text-accent" : ""}>{it.latestVersion}</span> : <span className="text-faint">—</span>}
        </td>
        <td className="px-3 py-2 whitespace-nowrap">
          <span className={clsx("rounded px-1.5 py-0.5 text-[11px] font-medium", st.cls)} title={it.error || it.detail}>
            {it.status === "outdated" ? <ArrowUpCircle size={11} className="mr-1 inline" /> : it.status === "current" ? <CheckCircle2 size={11} className="mr-1 inline" /> : it.status === "error" ? <AlertTriangle size={11} className="mr-1 inline" /> : null}
            {st.label}
          </span>
        </td>
        <td className="px-3 py-2 text-xs whitespace-nowrap text-muted" title={it.checkedAt ? fmtDateTime(it.checkedAt) : ""}>
          {it.checkedAt ? ago(it.checkedAt) : "—"}
        </td>
        <td className="px-3 py-2 text-right">
          <button
            className="btn-ghost px-2 py-1 text-xs"
            title="Configure how updates are found"
            onClick={(e) => {
              e.stopPropagation();
              onConfigure();
            }}
          >
            <Settings2 size={13} /> <span className="hidden xl:inline">Configure</span>
          </button>
        </td>
      </tr>
      {open && (
        <tr className="border-b border-line bg-panel/60">
          <td colSpan={8} className="px-6 py-4">
            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_280px]">
              <div className="grid content-start gap-2 text-[13px]">
                {it.error && (
                  <p className="flex items-start gap-2 text-down">
                    <AlertTriangle size={14} className="mt-0.5 shrink-0" /> {it.error}
                  </p>
                )}
                {it.detail && <p className="text-muted">{it.detail}</p>}
                {it.status === "untracked" && !it.detail && <p className="text-muted">Not linked to a source yet. Use Configure to pick Docker, a Helper Script or a GitHub repo.</p>}
                {it.releaseNotes ? (
                  <div>
                    <div className="mb-1 text-[11px] font-medium tracking-wide text-muted uppercase">
                      Release notes {it.latestVersion && `· ${it.latestVersion}`} {it.publishedAt && <span className="normal-case">({ago(it.publishedAt)})</span>}
                    </div>
                    <div className="max-h-80 overflow-auto rounded-md border border-line bg-black/20 px-4 py-3 text-xs">
                      <MarkdownPreview source={it.releaseNotes} />
                    </div>
                  </div>
                ) : (
                  it.usedSource === "docker" &&
                  !it.resolvedRepo && <p className="text-xs text-faint">Tip: add the project's GitHub repo in Configure to see release notes here.</p>
                )}
              </div>
              <div className="grid content-start gap-2 text-xs">
                <div className="grid grid-cols-[80px_1fr] gap-y-1 text-muted">
                  {it.image && (
                    <>
                      <span>Image</span>
                      <span className="truncate font-mono text-fg" title={it.image}>
                        {it.image}
                      </span>
                    </>
                  )}
                  {it.resolvedRepo && (
                    <>
                      <span>Repo</span>
                      <a href={`https://github.com/${it.resolvedRepo}`} target="_blank" rel="noreferrer noopener" className="truncate text-accent hover:underline">
                        {it.resolvedRepo}
                      </a>
                    </>
                  )}
                  {it.ignoredVersion && (
                    <>
                      <span>Skipped</span>
                      <span className="font-mono text-fg">{it.ignoredVersion}</span>
                    </>
                  )}
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {it.latestUrl && (
                    <a href={it.latestUrl} target="_blank" rel="noreferrer noopener" className="btn-outline py-1 text-xs">
                      <ExternalLink size={12} /> Release page
                    </a>
                  )}
                  {it.status === "outdated" && it.latestVersion && !it.latestVersion.endsWith("(newer image)") && (
                    <button className="btn-outline py-1 text-xs" disabled={save.isPending} onClick={skip} title="Hide this version; you'll hear about the next one">
                      <SkipForward size={12} /> Skip {it.latestVersion}
                    </button>
                  )}
                  <button className="btn-outline py-1 text-xs" disabled={check.isPending} onClick={() => check.mutate(it.serviceId)}>
                    {check.isPending ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />} Re-check
                  </button>
                </div>
                {(save.error || check.error) && <span className="text-down">{(save.error || check.error)?.message}</span>}
              </div>
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function ProxmoxNodes({ nodes, at }: { nodes: NodeUpdates[]; at?: string }) {
  const [open, setOpen] = useState<string | null>(null);
  return (
    <div className="rounded-lg border border-line bg-panel">
      <div className="flex items-center gap-2 border-b border-line px-4 py-2.5 text-sm">
        <Server size={14} className="text-muted" />
        <b>Proxmox nodes</b>
        <span className="text-xs text-muted">pending apt updates{at ? ` · checked ${ago(at)}` : ""}</span>
      </div>
      {nodes.map((n, i) => {
        const key = n.node || `${n.connection}-${i}`;
        return (
          <div key={key} className="border-b border-line/60 last:border-0">
            <button className="flex w-full items-center gap-3 px-4 py-2 text-left text-[13px] hover:bg-white/[0.03]" onClick={() => n.packages.length > 0 && setOpen(open === key ? null : key)}>
              <ChevronRight size={13} className={clsx("text-faint transition-transform", open === key && "rotate-90", n.packages.length === 0 && "invisible")} />
              <span className="font-medium">{n.node || n.connection}</span>
              {n.pveVersion && <span className="font-mono text-xs text-muted">PVE {n.pveVersion}</span>}
              <span className="ml-auto text-xs">
                {n.error ? (
                  <span className="text-down">{n.error}</span>
                ) : n.packages.length > 0 ? (
                  <span className="text-accent">
                    {n.packages.length} package update{n.packages.length === 1 ? "" : "s"}
                  </span>
                ) : (
                  <span className="text-up">Up to date</span>
                )}
              </span>
            </button>
            {open === key && (
              <div className="max-h-72 overflow-auto px-10 pb-3">
                <table className="w-full text-xs">
                  <tbody>
                    {n.packages.map((p) => (
                      <tr key={p.package} className="border-b border-line/40 last:border-0">
                        <td className="py-1 pr-3 font-mono">{p.package}</td>
                        <td className="py-1 pr-3 font-mono text-muted">
                          {p.oldVersion} → <span className="text-fg">{p.version}</span>
                        </td>
                        <td className="truncate py-1 text-faint">{p.title}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
