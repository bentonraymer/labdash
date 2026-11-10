import type { ReactNode } from "react";
import { NavLink, Outlet, useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { Activity, ArrowUpCircle, Cpu, FileText, LayoutGrid, LogOut, Network, Search, Server, Settings, Share2, ShieldAlert, Table, type LucideIcon } from "lucide-react";
import { api } from "@/lib/api";
import { useLaunchpad, useMe } from "@/lib/queries";
import { ModeToggle } from "@/components/ModeToggle";
import { useUI } from "@/components/UIProvider";

const nav: { to: string; label: string; icon: LucideIcon; group?: string }[] = [
  { to: "/", label: "Launchpad", icon: LayoutGrid },
  { to: "/services", label: "Services", icon: Table, group: "Inventory" },
  { to: "/hosts", label: "Hosts", icon: Cpu, group: "Inventory" },
  { to: "/status", label: "Status", icon: Activity, group: "Inventory" },
  { to: "/topology", label: "Topology", icon: Share2, group: "Inventory" },
  { to: "/updates", label: "Updates", icon: ArrowUpCircle, group: "Inventory" },
  { to: "/network/devices", label: "Devices", icon: Network, group: "Network" },
  { to: "/network/ips", label: "IP Addresses", icon: Server, group: "Network" },
  { to: "/network/vulns", label: "Vulnerabilities", icon: ShieldAlert, group: "Network" },
  { to: "/docs", label: "Docs", icon: FileText, group: "Knowledge" },
  { to: "/settings", label: "Settings", icon: Settings, group: "System" },
];

/** Dense, dark "technical" shell for everything except the launchpad. */
export function AdminLayout() {
  const ui = useUI();
  const { data: me } = useMe();
  const { data } = useLaunchpad();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const logout = async () => {
    await api.post("/api/auth/logout");
    qc.clear();
    navigate("/");
    window.location.reload();
  };

  let lastGroup: string | undefined;
  return (
    <div className="flex h-full">
      <aside className="flex w-56 shrink-0 flex-col border-r border-line bg-panel">
        <div className="flex items-center gap-2.5 px-4 py-4">
          <img src="/favicon.svg" alt="" className="size-6" />
          <span className="font-semibold tracking-tight">{data?.settings.site_title ?? "LabDash"}</span>
        </div>
        <button onClick={ui.openSpotlight} className="mx-3 mb-3 flex items-center gap-2 rounded-md border border-line-2 bg-bg/50 px-2.5 py-1.5 text-left text-xs text-faint hover:text-muted">
          <Search size={13} />
          <span className="flex-1">Search</span>
          <span className="kbd">⌘K</span>
        </button>
        <nav className="flex-1 overflow-y-auto px-2 text-sm">
          {nav.map((n) => {
            const header = n.group && n.group !== lastGroup ? n.group : null;
            lastGroup = n.group;
            return (
              <div key={n.to}>
                {header && <div className="px-2 pt-4 pb-1 text-[10px] font-semibold tracking-widest text-faint uppercase">{header}</div>}
                <NavLink
                  to={n.to}
                  end={n.to === "/"}
                  className={({ isActive }) =>
                    clsx("flex items-center gap-2.5 rounded-md px-2.5 py-1.5 transition", isActive ? "bg-accent/15 text-fg" : "text-muted hover:bg-white/5 hover:text-fg")
                  }
                >
                  <n.icon size={15} />
                  {n.label}
                </NavLink>
              </div>
            );
          })}
        </nav>
        <div className="border-t border-line p-3">
          <div className="mb-3 flex items-center justify-between text-xs text-muted">
            <span>Access mode</span>
            <ModeToggle variant="panel" />
          </div>
          <div className="flex items-center justify-between text-xs">
            <span className="truncate text-muted">{me?.username}</span>
            <button onClick={logout} className="btn-ghost px-2 py-1 text-xs" title="Sign out">
              <LogOut size={13} />
            </button>
          </div>
        </div>
      </aside>
      <main className="min-w-0 flex-1 overflow-y-auto bg-bg">
        <Outlet />
      </main>
    </div>
  );
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: string; actions?: ReactNode }) {
  return (
    <div className="flex items-end justify-between gap-4 border-b border-line px-6 py-4">
      <div>
        <h1 className="text-lg font-semibold tracking-tight">{title}</h1>
        {subtitle && <p className="mt-0.5 text-xs text-muted">{subtitle}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}

export function ComingSoon({ title, phase, children }: { title: string; phase: number; children: ReactNode }) {
  return (
    <>
      <PageHeader title={title} />
      <div className="p-6">
        <div className="max-w-2xl rounded-lg border border-dashed border-line-2 bg-panel/50 p-6">
          {phase > 0 && <div className="mb-2 inline-block rounded bg-accent/15 px-2 py-0.5 text-[11px] font-semibold text-accent">Build phase {phase}</div>}
          <div className="text-sm leading-relaxed text-muted">{children}</div>
        </div>
      </div>
    </>
  );
}
