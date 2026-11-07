import { useEffect, useMemo, useState } from "react";
import { Router } from "lucide-react";
import { useDevices, useHosts, useReserveOnRouter, useRouter } from "@/lib/queries";

/** Best guess at the MAC for an IP: the host record, a scanned device, or the router's client list. */
export function useMacFor() {
  const { data: hosts = [] } = useHosts();
  const { data: devices = [] } = useDevices();
  const { data: router } = useRouter();
  return useMemo(
    () =>
      (ip: string, hostId?: number | null): string => {
        if (hostId) {
          const h = hosts.find((x) => x.id === hostId);
          if (h?.mac && (!ip || h.ip === ip)) return h.mac;
        }
        const byIp = hosts.find((h) => h.ip === ip && h.mac);
        if (byIp) return byIp.mac;
        const dev = devices.find((d) => d.ip === ip && d.mac);
        if (dev) return dev.mac;
        const client = router?.clients.find((c) => c.ip === ip);
        return client?.mac ?? "";
      },
    [hosts, devices, router],
  );
}

export const isMac = (v: string) => /^([0-9a-f]{2}[:-]){5}[0-9a-f]{2}$/i.test(v.trim());

/** "Reserve on router" box for one IP (port view). */
export function RouterReserve({ ip, defaultName }: { ip: string; defaultName?: string }) {
  const { data: router } = useRouter();
  const macFor = useMacFor();
  const reserve = useReserveOnRouter();
  const [mac, setMac] = useState("");
  const [name, setName] = useState(defaultName ?? "");
  useEffect(() => setMac((m) => m || macFor(ip)), [ip, macFor]);
  useEffect(() => setName((n) => n || defaultName || ""), [defaultName]);
  if (!router?.enabled) return null;
  const existing = router.reservations.find((r) => r.ip === ip);

  return (
    <div className="grid gap-2 rounded-lg border border-line bg-panel p-4">
      <h2 className="flex items-center gap-2 text-sm font-semibold">
        <Router size={14} /> Router reservation
      </h2>
      {router.error && <p className="text-xs text-down">{router.error}</p>}
      {existing ? (
        <p className="text-xs text-muted">
          Reserved on the router for <span className="font-mono text-fg">{existing.mac}</span>
          {existing.name ? ` (${existing.name})` : ""}.
        </p>
      ) : (
        <>
          <input className="input py-1.5 font-mono" placeholder="MAC address" value={mac} onChange={(e) => setMac(e.target.value)} />
          <input className="input py-1.5" placeholder="Name on the router" value={name} onChange={(e) => setName(e.target.value)} />
          <button className="btn-primary py-1.5" disabled={!isMac(mac) || reserve.isPending} onClick={() => reserve.mutate({ ip, mac: mac.trim(), name: name.trim() })}>
            Reserve {ip} on router
          </button>
          {reserve.error && <p className="text-xs text-down">{reserve.error.message}</p>}
          {reserve.data && <p className="text-xs text-up">{reserve.data.detail}</p>}
          {!mac && <p className="text-[11px] text-faint">No MAC known for this IP yet. It appears once the device is scanned or connects to the router.</p>}
        </>
      )}
    </div>
  );
}
