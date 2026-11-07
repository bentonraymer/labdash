import { useEffect, useState } from "react";
import { Globe } from "lucide-react";
import { useCreateProxyHost, useNPMStatus } from "@/lib/queries";
import type { NPMCert } from "@/lib/types";
import { NPMBadge, certCovers, hostOf } from "./IntegrationBits";

/** Certificate dropdown, preselecting one that covers the domain. */
export function CertPicker({ certs, domain, value, onChange }: { certs: NPMCert[]; domain: string; value: number; onChange: (v: number) => void }) {
  return (
    <select className="input w-auto py-1 text-xs" value={value} onChange={(e) => onChange(Number(e.target.value))}>
      <option value={0}>No SSL certificate (HTTP only)</option>
      {certs.map((c) => (
        <option key={c.id} value={c.id}>
          {certCovers(c.domains, domain) ? "✓ " : ""}
          {c.name || c.domains.join(", ")}
        </option>
      ))}
    </select>
  );
}

export function defaultCert(certs: NPMCert[], domain: string): number {
  return certs.find((c) => certCovers(c.domains, domain))?.id ?? 0;
}

/** Shows a service's NPM proxy-host status and offers to create a missing one. */
export function NPMPanel({ serviceId, dnsUrl, hasDirect }: { serviceId: number; dnsUrl: string; hasDirect: boolean }) {
  const { data } = useNPMStatus();
  const create = useCreateProxyHost();
  const domain = hostOf(dnsUrl);
  const certs = data?.certificates ?? [];
  const [cert, setCert] = useState(0);
  useEffect(() => setCert(defaultCert(certs, domain)), [certs, domain]);
  if (!data?.enabled) return null;
  const st = data.services?.[serviceId];

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-line px-3 py-2.5 text-xs">
      <Globe size={14} className="text-muted" />
      <span className="font-medium">Nginx Proxy Manager</span>
      {data.error ? <span className="text-down">{data.error}</span> : <NPMBadge status={st} />}
      {st?.state === "ok" && <span className="font-mono text-faint">→ {st.forward}</span>}
      {st?.state === "mismatch" && <span className="text-warn">{st.detail}</span>}
      {st?.state === "missing" && (
        <>
          {hasDirect ? (
            <>
              <CertPicker certs={certs} domain={domain} value={cert} onChange={setCert} />
              <button
                type="button"
                className="btn-outline py-1 text-xs"
                disabled={create.isPending}
                onClick={() => create.mutate({ serviceId, certificateId: cert, sslForced: cert > 0, websockets: true })}
              >
                Create proxy host
              </button>
            </>
          ) : (
            <span className="text-faint">Add a direct IP and port so NPM knows where to forward.</span>
          )}
        </>
      )}
      {create.error && <span className="w-full text-down">{create.error.message}</span>}
      {create.data && <span className="w-full text-up">Created {create.data.detail}</span>}
    </div>
  );
}
