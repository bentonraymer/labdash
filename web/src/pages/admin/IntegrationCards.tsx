import { useEffect, useState, type ReactNode } from "react";
import { PlugZap } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { useNPMSettings, useProxmoxSettings, useSaveIntegration } from "@/lib/queries";
import type { NPMSettings, ProxmoxSettings } from "@/lib/types";

const row = "grid grid-cols-[220px_1fr] items-center gap-4";

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className={row}>
      <div>
        <div className="text-sm">{label}</div>
        {hint && <div className="text-xs text-faint">{hint}</div>}
      </div>
      {children}
    </div>
  );
}

function TestResult({ result }: { result: { ok: boolean; text: string } | null }) {
  if (!result) return null;
  return <span className={`text-sm ${result.ok ? "text-up" : "text-down"}`}>{result.text}</span>;
}

/** Nginx Proxy Manager credentials (password stored encrypted). */
export function NPMCard() {
  const { data } = useNPMSettings();
  const save = useSaveIntegration<NPMSettings>("npm");
  const [url, setUrl] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [verifyTls, setVerifyTls] = useState(false);
  const [enabled, setEnabled] = useState(true);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (!data) return;
    setUrl(data.url);
    setEmail(data.email);
    setVerifyTls(data.verifyTls);
    setEnabled(data.configured ? data.enabled : true);
  }, [data]);

  const test = async () => {
    setTesting(true);
    setResult(null);
    try {
      const r = await api.post<{ proxyHosts: number; certificates: number }>("/api/integrations/npm/test");
      setResult({ ok: true, text: `Connected: ${r.proxyHosts} proxy hosts, ${r.certificates} certificates` });
    } catch (e) {
      setResult({ ok: false, text: e instanceof ApiError ? e.message : "Failed" });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted">
        LabDash checks that each service's DNS name has a proxy host and can create missing ones. It never edits or deletes existing hosts. Use a dedicated NPM user if you like.
      </p>
      <Field label="NPM admin URL" hint="The port-81 admin UI">
        <input className="input max-w-sm font-mono" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://192.168.8.5:81" />
      </Field>
      <Field label="Email">
        <input className="input max-w-sm" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="off" />
      </Field>
      <Field label="Password" hint="Stored encrypted">
        <input
          className="input max-w-sm"
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder={data?.hasPassword ? "saved — leave blank to keep" : ""}
        />
      </Field>
      <Field label="Options">
        <div className="flex gap-5 text-sm text-muted">
          <label className="flex items-center gap-2">
            <input type="checkbox" className="accent-accent" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            Enabled
          </label>
          <label className="flex items-center gap-2">
            <input type="checkbox" className="accent-accent" checked={verifyTls} onChange={(e) => setVerifyTls(e.target.checked)} />
            Verify TLS certificate
          </label>
        </div>
      </Field>
      <div className="flex items-center gap-3">
        <button className="btn-primary" disabled={save.isPending} onClick={() => save.mutate({ url, email, password, verifyTls, enabled }, { onSuccess: () => setPassword("") })}>
          Save
        </button>
        <button className="btn-outline" disabled={testing || !data?.configured} onClick={test}>
          <PlugZap size={14} /> {testing ? "Testing…" : "Test connection"}
        </button>
        {save.error && <span className="text-sm text-down">{save.error.message}</span>}
        <TestResult result={result} />
      </div>
    </div>
  );
}

/** Proxmox read-only API token for live stats. */
export function ProxmoxCard() {
  const { data } = useProxmoxSettings();
  const save = useSaveIntegration<ProxmoxSettings>("proxmox");
  const [url, setUrl] = useState("");
  const [tokenId, setTokenId] = useState("");
  const [secret, setSecret] = useState("");
  const [verifyTls, setVerifyTls] = useState(false);
  const [enabled, setEnabled] = useState(true);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (!data) return;
    setUrl(data.url);
    setTokenId(data.tokenId);
    setVerifyTls(data.verifyTls);
    setEnabled(data.configured ? data.enabled : true);
  }, [data]);

  const test = async () => {
    setTesting(true);
    setResult(null);
    try {
      const r = await api.post<{ nodes: number; guests: number }>("/api/integrations/proxmox/test");
      setResult({ ok: true, text: `Connected: ${r.nodes} nodes, ${r.guests} VMs/LXCs` });
    } catch (e) {
      setResult({ ok: false, text: e instanceof ApiError ? e.message : "Failed" });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted">
        Read-only stats for nodes and for VMs/LXCs you've linked by VMID. Create a token in Datacenter → Permissions → API Tokens and give it the <span className="font-mono text-fg">PVEAuditor</span> role on{" "}
        <span className="font-mono text-fg">/</span>.
      </p>
      <Field label="Proxmox URL" hint="Any node in the cluster">
        <input className="input max-w-sm font-mono" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://192.168.8.11:8006" />
      </Field>
      <Field label="Token ID">
        <input className="input max-w-sm font-mono" value={tokenId} onChange={(e) => setTokenId(e.target.value)} placeholder="labdash@pve!dash" autoComplete="off" />
      </Field>
      <Field label="Token secret" hint="Stored encrypted">
        <input
          className="input max-w-sm font-mono"
          type="password"
          autoComplete="new-password"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          placeholder={data?.hasSecret ? "saved — leave blank to keep" : "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"}
        />
      </Field>
      <Field label="Options">
        <div className="flex gap-5 text-sm text-muted">
          <label className="flex items-center gap-2">
            <input type="checkbox" className="accent-accent" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            Enabled
          </label>
          <label className="flex items-center gap-2">
            <input type="checkbox" className="accent-accent" checked={verifyTls} onChange={(e) => setVerifyTls(e.target.checked)} />
            Verify TLS (off for self-signed)
          </label>
        </div>
      </Field>
      <div className="flex items-center gap-3">
        <button className="btn-primary" disabled={save.isPending} onClick={() => save.mutate({ url, tokenId, tokenSecret: secret, verifyTls, enabled }, { onSuccess: () => setSecret("") })}>
          Save
        </button>
        <button className="btn-outline" disabled={testing || !data?.configured} onClick={test}>
          <PlugZap size={14} /> {testing ? "Testing…" : "Test connection"}
        </button>
        {save.error && <span className="text-sm text-down">{save.error.message}</span>}
        <TestResult result={result} />
      </div>
    </div>
  );
}
