import clsx from "clsx";
import { useHosts } from "@/lib/queries";
import { hostKindLabel, runtimeIcon, runtimeLabel } from "@/lib/labels";
import type { DeploymentInput } from "@/lib/queries";
import type { Hosting, Runtime } from "@/lib/types";

export interface HostingForm {
  hostId: string;
  runtime: Runtime | "";
  vmid: string;
  containerName: string;
}

export const emptyHosting: HostingForm = { hostId: "", runtime: "", vmid: "", containerName: "" };

export const hostingFromService = (h: Hosting | null | undefined): HostingForm =>
  h ? { hostId: h.hostId ? String(h.hostId) : "", runtime: h.runtime, vmid: h.vmid ? String(h.vmid) : "", containerName: h.containerName } : emptyHosting;

/** null means "no hosting info". */
export const hostingToInput = (f: HostingForm): DeploymentInput | null =>
  f.runtime
    ? { hostId: f.hostId ? Number(f.hostId) : null, runtime: f.runtime, vmid: f.vmid ? Number(f.vmid) : null, containerName: f.containerName.trim() }
    : null;

const runtimes: Runtime[] = ["docker", "lxc", "vm", "bare_metal", "other"];

/** Where a service runs: runtime, host, VMID / container name. */
export function HostingFields({ value: f, onChange }: { value: HostingForm; onChange: (f: HostingForm) => void }) {
  const { data: hosts = [] } = useHosts();
  const set = <K extends keyof HostingForm>(k: K, v: HostingForm[K]) => onChange({ ...f, [k]: v });
  const guest = f.runtime === "vm" || f.runtime === "lxc";
  const bare = f.runtime === "bare_metal";
  // VMs/LXCs run on a Proxmox node; Docker runs on any host (often a VM).
  const hostChoices = guest ? hosts.filter((h) => h.kind === "proxmox_node" || h.kind === "server") : hosts;

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap gap-1.5">
        {runtimes.map((r) => {
          const Icon = runtimeIcon[r];
          return (
            <button
              type="button"
              key={r}
              onClick={() => set("runtime", f.runtime === r ? "" : r)}
              className={clsx(
                "flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition",
                f.runtime === r ? "border-accent bg-accent/15 text-fg" : "border-line-2 text-muted hover:text-fg",
              )}
            >
              <Icon size={13} /> {runtimeLabel[r]}
            </button>
          );
        })}
        {!f.runtime && <span className="self-center text-xs text-faint">Pick how it runs (optional)</span>}
      </div>
      {f.runtime && (
        <div className="grid grid-cols-[1fr_120px_1fr] gap-3">
          <div>
            <label className="label">{guest ? "Proxmox node" : bare ? "Which machine" : "Runs on host"}</label>
            <select className="input" value={f.hostId} onChange={(e) => set("hostId", e.target.value)}>
              <option value="">— Choose —</option>
              {hostChoices.map((h) => (
                <option key={h.id} value={h.id}>
                  {h.name} · {hostKindLabel[h.kind]}
                  {h.nodeName && h.nodeName !== h.name ? ` on ${h.nodeName}` : ""}
                </option>
              ))}
            </select>
            {hosts.length === 0 && <p className="mt-1 text-[11px] text-faint">Add hosts on the Hosts page first.</p>}
            {bare && f.hostId && <p className="mt-1 text-[11px] text-faint">This service is the machine itself (e.g. the TrueNAS web UI on the NAS). It uses the host's IP.</p>}
          </div>
          <div>
            <label className="label">VMID</label>
            <input className="input font-mono" inputMode="numeric" disabled={!guest} value={f.vmid} onChange={(e) => set("vmid", e.target.value.replace(/\D/g, ""))} placeholder={guest ? "105" : "—"} />
          </div>
          <div>
            <label className="label">Container name</label>
            <input
              className="input font-mono"
              disabled={f.runtime !== "docker"}
              value={f.containerName}
              onChange={(e) => set("containerName", e.target.value)}
              placeholder={f.runtime === "docker" ? "jellyfin" : "—"}
            />
          </div>
        </div>
      )}
    </div>
  );
}
