import { useState } from "react";
import { ExternalLink, Power, Terminal } from "lucide-react";
import { ApiError } from "@/lib/api";
import { useProxmoxSettings, wakeOnLan } from "@/lib/queries";
import type { Host } from "@/lib/types";

/** Wake-on-LAN button with inline result. */
export function WakeButton({ mac, ip, compact }: { mac: string; ip?: string; compact?: boolean }) {
  const [state, setState] = useState<"idle" | "sent" | "error">("idle");
  const [err, setErr] = useState("");
  if (!mac) return null;
  return (
    <button
      type="button"
      className={compact ? "btn-ghost p-1.5" : "btn-outline py-1 text-xs"}
      title={state === "error" ? err : `Send Wake-on-LAN to ${mac}`}
      onClick={async (e) => {
        e.stopPropagation();
        try {
          await wakeOnLan(mac, ip);
          setState("sent");
          setTimeout(() => setState("idle"), 3000);
        } catch (x) {
          setErr(x instanceof ApiError ? x.message : "Failed");
          setState("error");
        }
      }}
    >
      <Power size={13} className={state === "sent" ? "text-up" : state === "error" ? "text-down" : ""} />
      {!compact && (state === "sent" ? "Packet sent" : state === "error" ? "Failed" : "Wake")}
    </button>
  );
}

/** Generated quick links for a host: Proxmox web UI, guest console, SSH. */
export function useHostLinks() {
  const { data: pve } = useProxmoxSettings();
  return (h: Host, byId: Map<number, Host>): { label: string; url: string; icon: typeof ExternalLink }[] => {
    const links: { label: string; url: string; icon: typeof ExternalLink }[] = [];
    if (h.kind === "proxmox_node" && h.ip) links.push({ label: "Proxmox UI", url: `https://${h.ip}:8006`, icon: ExternalLink });
    if ((h.kind === "vm" || h.kind === "lxc") && h.vmid) {
      // Console through the parent node (or the configured API URL).
      const parent = h.parentId ? byId.get(h.parentId) : undefined;
      const base = parent?.ip ? `https://${parent.ip}:8006` : pve?.url?.replace(/\/+$/, "");
      if (base) {
        const type = h.kind === "lxc" ? "lxc" : "qemu";
        const node = parent?.name ?? "";
        links.push({ label: "Console", url: `${base}/?console=${type === "lxc" ? "lxc" : "kvm"}&vmid=${h.vmid}&node=${encodeURIComponent(node)}&novnc=1`, icon: ExternalLink });
      }
    }
    if (h.ip && h.kind !== "device") links.push({ label: "SSH", url: `ssh://root@${h.ip}`, icon: Terminal });
    return links;
  };
}
