import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import {
  Activity,
  BellOff,
  Bell,
  CornerDownLeft,
  FileText,
  Globe,
  LayoutGrid,
  Network,
  Plus,
  Search,
  Server,
  Settings as SettingsIcon,
  Share2,
  Table,
  type LucideIcon,
} from "lucide-react";
import { fuzzyMatch, frecencyBoost } from "@/lib/fuzzy";
import { api } from "@/lib/api";
import { keys, useLaunchpad, usePatchSettings, useSpotlightUsage } from "@/lib/queries";
import type { Service } from "@/lib/types";
import { ServiceIcon } from "./ServiceIcon";
import { StatusDot } from "./StatusDot";
import { useUI } from "./UIProvider";

type Result =
  | { kind: "service"; id: string; title: string; subtitle: string; score: number; indices: number[]; service: Service }
  | { kind: "action"; id: string; title: string; subtitle: string; score: number; indices: number[]; icon: LucideIcon; run: () => void };

const groupLabel: Record<Result["kind"], string> = { service: "Services", action: "Actions" };

export function Spotlight({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const { data } = useLaunchpad();
  const { data: usage } = useSpotlightUsage();
  const patch = usePatchSettings();
  const navigate = useNavigate();
  const ui = useUI();
  const qc = useQueryClient();

  useEffect(() => {
    if (open) {
      setQ("");
      setSel(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  const settings = data?.settings;
  const usageMap = useMemo(() => new Map((usage ?? []).map((u) => [`${u.kind}:${u.ref}`, u])), [usage]);

  const actions = useMemo(() => {
    const go = (path: string) => () => navigate(path);
    const mode = settings?.access_mode ?? "dns";
    const list: { id: string; title: string; subtitle: string; icon: LucideIcon; run: () => void; keywords?: string }[] = [
      { id: "add-service", title: "Add service", subtitle: "Create a new service tile", icon: Plus, run: () => ui.editService(null), keywords: "new create" },
      { id: "add-section", title: "Add section", subtitle: "Create a launchpad section", icon: Plus, run: () => ui.editSection(null), keywords: "new group" },
      {
        id: "toggle-mode",
        title: mode === "dns" ? "Switch to Direct (IP) mode" : "Switch to DNS mode",
        subtitle: `Currently using ${mode === "dns" ? "DNS names" : "IP:port"} for every link`,
        icon: Globe,
        run: () => patch.mutate({ access_mode: mode === "dns" ? "direct" : "dns" }),
        keywords: "toggle access mode ip dns direct",
      },
      {
        id: "toggle-alerts",
        title: settings?.alerts_paused ? "Resume alerts" : "Pause all alerts",
        subtitle: "Pushover notifications",
        icon: settings?.alerts_paused ? Bell : BellOff,
        run: () => patch.mutate({ alerts_paused: !settings?.alerts_paused }),
        keywords: "mute silence notifications pushover",
      },
      { id: "go-launchpad", title: "Launchpad", subtitle: "Go to", icon: LayoutGrid, run: go("/"), keywords: "home dashboard" },
      { id: "go-services", title: "Services table", subtitle: "Go to", icon: Table, run: go("/services"), keywords: "inventory hosting" },
      { id: "go-status", title: "Status", subtitle: "Go to", icon: Activity, run: go("/status"), keywords: "uptime monitors" },
      { id: "go-devices", title: "Network devices", subtitle: "Go to", icon: Network, run: go("/network/devices"), keywords: "scan runzero" },
      { id: "go-ips", title: "IP addresses", subtitle: "Go to", icon: Server, run: go("/network/ips"), keywords: "ipam static" },
      { id: "go-docs", title: "Docs", subtitle: "Go to", icon: FileText, run: go("/docs"), keywords: "notes markdown" },
      { id: "go-topology", title: "Topology", subtitle: "Go to", icon: Share2, run: go("/topology"), keywords: "map graph" },
      { id: "go-settings", title: "Settings", subtitle: "Go to", icon: SettingsIcon, run: go("/settings"), keywords: "config preferences" },
    ];
    return list;
  }, [settings?.access_mode, settings?.alerts_paused, navigate, ui, patch]);

  const results = useMemo<Result[]>(() => {
    const out: Result[] = [];
    const query = q.trim();
    for (const s of data?.services ?? []) {
      const u = usageMap.get(`service:${s.id}`);
      const boost = frecencyBoost(u?.count ?? 0, u?.lastUsed);
      if (!query) {
        out.push({ kind: "service", id: `s${s.id}`, title: s.name, subtitle: s.url, score: boost, indices: [], service: s });
        continue;
      }
      const m = fuzzyMatch(query, s.name);
      // Secondary fields match at a discount and don't highlight the title.
      const extra = [s.description, ...s.tags, s.directIp, s.dnsUrl]
        .map((t) => fuzzyMatch(query, t))
        .reduce((best, x) => (x && x.score > best ? x.score : best), -1);
      const score = Math.max(m?.score ?? -1, extra > 0 ? extra * 0.5 : -1);
      if (score < 0) continue;
      out.push({ kind: "service", id: `s${s.id}`, title: s.name, subtitle: s.url, score: score + boost, indices: m?.indices ?? [], service: s });
    }
    for (const a of actions) {
      const u = usageMap.get(`action:${a.id}`);
      const boost = frecencyBoost(u?.count ?? 0, u?.lastUsed);
      if (!query) continue; // actions only appear when typing
      const m = fuzzyMatch(query, a.title);
      const kw = a.keywords ? fuzzyMatch(query, a.keywords) : null;
      const score = Math.max(m?.score ?? -1, kw ? kw.score * 0.6 : -1);
      if (score < 0) continue;
      out.push({ kind: "action", id: a.id, title: a.title, subtitle: a.subtitle, icon: a.icon, run: a.run, score: score * 0.9 + boost, indices: m?.indices ?? [] });
    }
    out.sort((a, b) => b.score - a.score || a.title.localeCompare(b.title));
    return out.slice(0, query ? 12 : 8);
  }, [q, data?.services, actions, usageMap]);

  useEffect(() => setSel(0), [q]);
  useEffect(() => {
    listRef.current?.querySelector(`[data-idx="${sel}"]`)?.scrollIntoView({ block: "nearest" });
  }, [sel]);

  const record = (kind: string, ref: string) => {
    api.post("/api/spotlight/usage", { kind, ref }).then(() => qc.invalidateQueries({ queryKey: keys.usage })).catch(() => {});
  };

  const activate = (r: Result | undefined, opts: { newTab?: boolean } = {}) => {
    if (!r) return;
    onClose();
    if (r.kind === "service") {
      record("service", String(r.service.id));
      if (!r.service.url) return ui.editService(r.service);
      const newTab = opts.newTab ?? r.service.newTab;
      if (newTab) window.open(r.service.url, "_blank", "noopener");
      else window.location.href = r.service.url;
    } else {
      record("action", r.id);
      r.run();
    }
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown" || (e.key === "n" && e.ctrlKey)) {
      e.preventDefault();
      setSel((s) => Math.min(s + 1, results.length - 1));
    } else if (e.key === "ArrowUp" || (e.key === "p" && e.ctrlKey)) {
      e.preventDefault();
      setSel((s) => Math.max(s - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      activate(results[sel], e.shiftKey ? { newTab: true } : {});
    } else if (e.key === "Escape") {
      onClose();
    } else if (e.key === "Tab" && results[0] && q && results[0].title.toLowerCase().startsWith(q.toLowerCase())) {
      // Tab completes the top hit like macOS Spotlight.
      e.preventDefault();
      setQ(results[0].title);
    }
  };

  if (!open) return null;

  const top = results[0];
  const ghost = q && top && top.title.toLowerCase().startsWith(q.toLowerCase()) ? q + top.title.slice(q.length) : "";

  let lastKind: string | null = null;
  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-start justify-center bg-black/45 px-4 pt-[18vh] backdrop-blur-md" onMouseDown={onClose}>
      <div className="glass-strong pop-in w-full max-w-[640px] overflow-hidden rounded-2xl shadow-[0_30px_80px_-20px_rgba(0,0,0,0.8)]" onMouseDown={(e) => e.stopPropagation()}>
        <div className="relative flex items-center gap-3 px-5 py-4">
          <Search size={22} className="shrink-0 text-muted" />
          <div className="relative flex-1">
            {/* Inline autocomplete ghost text */}
            <div aria-hidden className="pointer-events-none absolute inset-0 flex items-center text-[22px] font-light whitespace-pre text-white/25">
              <span className="invisible">{q}</span>
              <span>{ghost.slice(q.length)}</span>
            </div>
            <input
              ref={inputRef}
              value={q}
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={onKeyDown}
              placeholder="Search services and actions…"
              className="relative w-full bg-transparent text-[22px] font-light text-fg outline-none placeholder:text-white/30"
              spellCheck={false}
              autoComplete="off"
            />
          </div>
          <span className="kbd">esc</span>
        </div>

        {results.length > 0 && (
          <div ref={listRef} className="max-h-[52vh] overflow-y-auto border-t border-white/8 p-2">
            {results.map((r, i) => {
              const header = r.kind !== lastKind ? groupLabel[r.kind] : null;
              lastKind = r.kind;
              return (
                <div key={r.id}>
                  {header && <div className="px-3 pt-2 pb-1 text-[11px] font-semibold tracking-wider text-faint uppercase">{q ? header : "Recent & frequent"}</div>}
                  <button
                    data-idx={i}
                    onMouseMove={() => setSel(i)}
                    onClick={() => activate(r)}
                    className={clsx(
                      "flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-left transition-colors",
                      i === sel ? "bg-accent/90 text-white" : "text-fg hover:bg-white/5",
                    )}
                  >
                    {r.kind === "service" ? (
                      <ServiceIcon icon={r.service.icon} name={r.service.name} className="size-8 shrink-0" />
                    ) : (
                      <span className={clsx("flex size-8 shrink-0 items-center justify-center rounded-lg", i === sel ? "bg-white/20" : "bg-white/5")}>
                        <r.icon size={17} />
                      </span>
                    )}
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-[15px] font-medium">{highlight(r.title, r.indices, i === sel)}</div>
                      <div className={clsx("truncate font-mono text-xs", i === sel ? "text-white/75" : "text-muted")}>{r.subtitle || "No URL configured"}</div>
                    </div>
                    {r.kind === "service" && <StatusDot status={r.service.status} />}
                    {i === sel && <CornerDownLeft size={16} className="shrink-0 text-white/80" />}
                  </button>
                </div>
              );
            })}
          </div>
        )}
        {q && results.length === 0 && <div className="border-t border-white/8 px-5 py-6 text-center text-sm text-muted">No matches for “{q}”</div>}

        <div className="flex items-center gap-4 border-t border-white/8 px-5 py-2 text-[11px] text-faint">
          <span className="flex items-center gap-1">
            <span className="kbd">↑</span>
            <span className="kbd">↓</span> navigate
          </span>
          <span className="flex items-center gap-1">
            <span className="kbd">↵</span> open
          </span>
          <span className="flex items-center gap-1">
            <span className="kbd">⇧↵</span> new tab
          </span>
          <span className="flex items-center gap-1">
            <span className="kbd">tab</span> complete
          </span>
          <span className="ml-auto">
            Mode: <span className="font-medium text-muted">{settings?.access_mode === "direct" ? "Direct IP" : "DNS"}</span>
          </span>
        </div>
      </div>
    </div>,
    document.body,
  );
}

function highlight(text: string, indices: number[], selected: boolean): ReactNode {
  if (!indices.length) return text;
  const set = new Set(indices);
  return [...text].map((ch, i) =>
    set.has(i) ? (
      <span key={i} className={selected ? "font-bold underline decoration-white/50 underline-offset-2" : "font-semibold text-accent"}>
        {ch}
      </span>
    ) : (
      ch
    ),
  );
}
