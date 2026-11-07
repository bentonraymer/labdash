import { useEffect, useState, type ReactNode } from "react";
import { PlugZap, Plus, X } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { useNPMSettings, useProxmoxSettings, useRouterSettings, useSaveIntegration } from "@/lib/queries";
import type { NPMSettings, ProxmoxSettings, RouterSettings } from "@/lib/types";

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

type ConnDraft = { id: string; name: string; url: string; verifyTls: boolean; tokenId: string; tokenSecret: string; hasSecret: boolean };

/** Proxmox read-only API tokens for live stats, one per standalone node or cluster. */
export function ProxmoxCard() {
  const { data } = useProxmoxSettings();
  const save = useSaveIntegration<ProxmoxSettings>("proxmox");
  const [conns, setConns] = useState<ConnDraft[]>([]);
  const [enabled, setEnabled] = useState(true);
  const [results, setResults] = useState<{ name: string; ok: boolean; nodes: number; guests: number; error?: string }[] | null>(null);
  const [testErr, setTestErr] = useState("");
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (!data) return;
    setConns(data.connections.map((c) => ({ ...c, tokenSecret: "" })));
    setEnabled(data.configured ? data.enabled : true);
  }, [data]);

  const set = (i: number, patch: Partial<ConnDraft>) => setConns((cs) => cs.map((c, j) => (j === i ? { ...c, ...patch } : c)));
  const add = () => setConns((cs) => [...cs, { id: "", name: "", url: "", verifyTls: false, tokenId: "", tokenSecret: "", hasSecret: false }]);

  const test = async () => {
    setTesting(true);
    setResults(null);
    setTestErr("");
    try {
      setResults(await api.post("/api/integrations/proxmox/test"));
    } catch (e) {
      setTestErr(e instanceof ApiError ? e.message : "Failed");
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted">
        Read-only stats for nodes and for VMs/LXCs you've linked by VMID. Add one connection per <b>standalone</b> node; for a <b>cluster</b>, one connection to any member
        is enough. Create a token in Datacenter → Permissions → API Tokens with the <span className="font-mono text-fg">PVEAuditor</span> role on{" "}
        <span className="font-mono text-fg">/</span>.
      </p>
      {conns.map((c, i) => (
        <div key={c.id || `new-${i}`} className="grid gap-2 rounded-lg border border-line p-3">
          <div className="grid grid-cols-[160px_1fr_auto] gap-2">
            <input className="input py-1.5" value={c.name} onChange={(e) => set(i, { name: e.target.value })} placeholder="pve1" />
            <input className="input py-1.5 font-mono" value={c.url} onChange={(e) => set(i, { url: e.target.value })} placeholder="https://192.168.8.11:8006" />
            <button className="btn-ghost px-2 hover:text-down" title="Remove connection" onClick={() => setConns((cs) => cs.filter((_, j) => j !== i))}>
              <X size={14} />
            </button>
          </div>
          <div className="grid grid-cols-[1fr_1fr_auto] items-center gap-2">
            <input className="input py-1.5 font-mono" value={c.tokenId} onChange={(e) => set(i, { tokenId: e.target.value })} placeholder="labdash@pve!dash" autoComplete="off" />
            <input
              className="input py-1.5 font-mono"
              type="password"
              autoComplete="new-password"
              value={c.tokenSecret}
              onChange={(e) => set(i, { tokenSecret: e.target.value })}
              placeholder={c.hasSecret ? "secret saved — blank keeps it" : "token secret (UUID)"}
            />
            <label className="flex items-center gap-1.5 text-xs text-muted" title="Turn off for Proxmox's default self-signed certificate">
              <input type="checkbox" className="accent-accent" checked={c.verifyTls} onChange={(e) => set(i, { verifyTls: e.target.checked })} />
              Verify TLS
            </label>
          </div>
          {results?.find((r) => r.name === c.name) &&
            (() => {
              const r = results.find((x) => x.name === c.name)!;
              return <p className={`text-xs ${r.ok ? "text-up" : "text-down"}`}>{r.ok ? `Connected: ${r.nodes} node${r.nodes === 1 ? "" : "s"}, ${r.guests} VMs/LXCs` : r.error}</p>;
            })()}
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-3">
        <button className="btn-outline" onClick={add}>
          <Plus size={14} /> Add connection
        </button>
        <label className="flex items-center gap-2 text-sm text-muted">
          <input type="checkbox" className="accent-accent" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          Enabled
        </label>
      </div>
      <div className="flex items-center gap-3">
        <button
          className="btn-primary"
          disabled={save.isPending}
          onClick={() =>
            save.mutate(
              { enabled, connections: conns.map(({ id, name, url, verifyTls, tokenId, tokenSecret }) => ({ id, name, url, verifyTls, tokenId, tokenSecret })) },
              { onSuccess: () => setResults(null) },
            )
          }
        >
          Save
        </button>
        <button className="btn-outline" disabled={testing || !data?.configured} onClick={test}>
          <PlugZap size={14} /> {testing ? "Testing…" : "Test connections"}
        </button>
        {save.error && <span className="text-sm text-down">{save.error.message}</span>}
        {testErr && <span className="text-sm text-down">{testErr}</span>}
      </div>
    </div>
  );
}

/** GL.iNet router (firmware 4.x) for DHCP reservations and client names. */
export function GLiNetCard() {
  const { data } = useRouterSettings();
  const save = useSaveIntegration<RouterSettings>("glinet");
  const [url, setUrl] = useState("");
  const [username, setUsername] = useState("root");
  const [password, setPassword] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (!data) return;
    setUrl(data.url);
    setUsername(data.username || "root");
    setEnabled(data.configured ? data.enabled : true);
  }, [data]);

  const test = async () => {
    setTesting(true);
    setResult(null);
    try {
      const r = await api.post<{ reservations: number; clients: number }>("/api/integrations/glinet/test");
      setResult({ ok: true, text: `Connected: ${r.reservations} reservations, ${r.clients} clients` });
    } catch (e) {
      setResult({ ok: false, text: e instanceof ApiError ? e.message : "Failed" });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted">
        Reads the reserved addresses from the router's LAN page and its client list (names, online status), and can add new reservations from the Add Service wizard or the port
        view. Removing reservations isn't possible through the router's API, so do that in the router UI.
      </p>
      <Field label="Router URL">
        <input className="input max-w-sm font-mono" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://192.168.8.1" />
      </Field>
      <Field label="Username">
        <input className="input max-w-sm" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
      </Field>
      <Field label="Admin password" hint="Stored encrypted">
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
        <label className="flex items-center gap-2 text-sm text-muted">
          <input type="checkbox" className="accent-accent" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          Enabled
        </label>
      </Field>
      <div className="flex items-center gap-3">
        <button className="btn-primary" disabled={save.isPending} onClick={() => save.mutate({ url, username, password, enabled }, { onSuccess: () => setPassword("") })}>
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
