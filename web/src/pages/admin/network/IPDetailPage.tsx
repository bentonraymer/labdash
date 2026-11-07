import { useState, type FormEvent } from "react";
import { Link, useParams } from "react-router";
import clsx from "clsx";
import { ArrowLeft, Search, Trash2 } from "lucide-react";
import { api } from "@/lib/api";
import { useDeletePortClaim, useIPDetail, useLaunchpad, useSavePortClaim } from "@/lib/queries";
import { ServiceIcon } from "@/components/ServiceIcon";
import { PageHeader } from "../AdminLayout";
import { WakeButton } from "@/components/HostActions";
import { cellStyle, stateLabel } from "./ipStyles";

const sourceLabel = { service: "Service", claim: "Claimed", scan: "Scan only" } as const;

/** One IP: who owns it and every claimed / discovered port. */
export function IPDetailPage() {
  const { ip = "" } = useParams();
  const { data, error } = useIPDetail(ip);
  const { data: lp } = useLaunchpad();
  const save = useSavePortClaim();
  const del = useDeletePortClaim();
  const [port, setPort] = useState("");
  const [proto, setProto] = useState<"tcp" | "udp">("tcp");
  const [label, setLabel] = useState("");
  const [serviceId, setServiceId] = useState("");
  const [free, setFree] = useState<number | null>(null);
  const [from, setFrom] = useState("8000");

  if (error) return <div className="p-6 text-down">{(error as Error).message}</div>;
  if (!data) return null;

  const addClaim = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(
      { ip, port: Number(port), proto, label, serviceId: serviceId ? Number(serviceId) : null },
      {
        onSuccess: () => {
          setPort("");
          setLabel("");
        },
      },
    );
  };
  const findFree = async () => {
    const r = await api.get<{ port: number }>(`/api/ips/${ip}/free-port?from=${Number(from) || 8000}`);
    setFree(r.port);
  };

  const claimed = data.ports.filter((p) => p.source !== "scan").length;
  const scanned = data.ports.some((p) => p.open !== null);

  return (
    <>
      <PageHeader
        title={ip}
        subtitle={`${data.subnet ? `${data.subnet.name || data.subnet.cidr} · ` : ""}${claimed} claimed port${claimed === 1 ? "" : "s"}${data.mac ? ` · MAC ${data.mac}` : ""}`}
        actions={
          <>
          {data.mac && <WakeButton mac={data.mac} ip={ip} />}
          <Link to="/network/ips" className="btn-ghost py-1.5">
            <ArrowLeft size={15} /> All IPs
          </Link>
          </>
        }
      />
      <div className="grid gap-6 p-6 xl:grid-cols-[minmax(0,1fr)_320px]">
        <div className="grid content-start gap-4">
          <div className="flex flex-wrap items-center gap-2">
            <span className="flex items-center gap-2 rounded-md border border-line px-2.5 py-1 text-xs">
              <span className={clsx("size-2.5 rounded-sm", cellStyle[data.state])} />
              {stateLabel[data.state]}
            </span>
            {data.owners.map((o, i) => (
              <span key={i} className="flex items-center gap-1.5 rounded-md bg-white/5 px-2.5 py-1 text-xs">
                {o.kind === "service" && <ServiceIcon icon={o.icon ?? ""} name={o.label} className="size-3.5" />}
                {o.label} <span className="text-faint">{o.kind}</span>
              </span>
            ))}
          </div>

          <div className="overflow-hidden rounded-lg border border-line">
            <table className="w-full text-[13px]">
              <thead className="border-b border-line bg-panel text-xs text-muted">
                <tr>
                  <th className="px-3 py-2 text-left font-medium">Port</th>
                  <th className="px-3 py-2 text-left font-medium">Used by</th>
                  <th className="px-3 py-2 text-left font-medium">Source</th>
                  <th className="px-3 py-2 text-left font-medium">Last scan</th>
                  <th className="w-10" />
                </tr>
              </thead>
              <tbody>
                {data.ports.length === 0 && (
                  <tr>
                    <td colSpan={5} className="px-3 py-8 text-center text-muted">
                      No ports claimed yet. Services with this IP and a port appear here automatically.
                    </td>
                  </tr>
                )}
                {data.ports.map((p) => (
                  <tr key={`${p.port}/${p.proto}`} className="border-b border-line/70 last:border-0">
                    <td className="px-3 py-2 font-mono">
                      {p.port}
                      <span className="text-faint">/{p.proto}</span>
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex items-center gap-2">
                        {p.serviceId && <ServiceIcon icon={p.serviceIcon} name={p.serviceName} className="size-4" />}
                        <span>{p.serviceName || p.label || p.title || p.banner || "—"}</span>
                        {p.serviceName && p.label && <span className="text-xs text-faint">{p.label}</span>}
                      </div>
                    </td>
                    <td className="px-3 py-2 text-xs text-muted">{sourceLabel[p.source]}</td>
                    <td className="px-3 py-2 text-xs">
                      {p.open === null ? (
                        <span className="text-faint">not scanned</span>
                      ) : p.open ? (
                        <span className="text-up">● open</span>
                      ) : (
                        <span className={p.source !== "scan" ? "text-warn" : "text-faint"}>○ closed{p.source !== "scan" ? " (claimed!)" : ""}</span>
                      )}
                    </td>
                    <td className="px-2">
                      {p.claimId && (
                        <button className="btn-ghost p-1" title="Remove claim" onClick={() => del.mutate(p.claimId!)}>
                          <Trash2 size={13} />
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {!scanned && <p className="text-xs text-faint">Open/closed status appears once network scanning (build phase 5) has scanned this address.</p>}
        </div>

        <aside className="grid content-start gap-4">
          <form onSubmit={addClaim} className="grid gap-2 rounded-lg border border-line bg-panel p-4">
            <h2 className="text-sm font-semibold">Claim a port</h2>
            <div className="grid grid-cols-[1fr_80px] gap-2">
              <input className="input py-1.5 font-mono" inputMode="numeric" placeholder="8920" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, "").slice(0, 5))} />
              <select className="input py-1.5" value={proto} onChange={(e) => setProto(e.target.value as "tcp" | "udp")}>
                <option value="tcp">tcp</option>
                <option value="udp">udp</option>
              </select>
            </div>
            <input className="input py-1.5" placeholder="Label (e.g. HTTPS, metrics)" value={label} onChange={(e) => setLabel(e.target.value)} />
            <select className="input py-1.5" value={serviceId} onChange={(e) => setServiceId(e.target.value)}>
              <option value="">— No service —</option>
              {lp?.services
                .slice()
                .sort((a, b) => a.name.localeCompare(b.name))
                .map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
            </select>
            <button className="btn-primary py-1.5" disabled={!port || save.isPending}>
              Claim
            </button>
            {save.error && <p className="text-xs text-down">{save.error.message}</p>}
          </form>

          <div className="grid gap-2 rounded-lg border border-line bg-panel p-4">
            <h2 className="text-sm font-semibold">Find a free port</h2>
            <div className="flex gap-2">
              <input className="input py-1.5 font-mono" inputMode="numeric" value={from} onChange={(e) => setFrom(e.target.value.replace(/\D/g, ""))} />
              <button className="btn-outline shrink-0 py-1.5" onClick={findFree}>
                <Search size={14} /> From here
              </button>
            </div>
            {free && (
              <p className="text-sm">
                Next free: <span className="font-mono font-semibold text-up">{free}</span>
              </p>
            )}
          </div>
        </aside>
      </div>
    </>
  );
}
