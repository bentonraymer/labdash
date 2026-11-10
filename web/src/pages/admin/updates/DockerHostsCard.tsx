import { useState } from "react";
import { Check, Copy, Plus, Trash2 } from "lucide-react";
import { useDeleteDockerEndpoint, useHosts, useSaveDockerEndpoint, useSaveGitHubToken, useUpdates, testDockerEndpoint } from "@/lib/queries";
import type { DockerEndpoint } from "@/lib/types";

const compose = `services:
  docker-socket-proxy:
    image: lscr.io/linuxserver/socket-proxy:latest
    container_name: docker-socket-proxy
    restart: unless-stopped
    environment:
      CONTAINERS: 1   # list containers
      IMAGES: 1       # read image details
      POST: 0         # read-only: nothing can be changed
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    read_only: true
    tmpfs:
      - /run
    ports:
      - "2375:2375"`;

type Draft = { id?: number; name: string; url: string; hostId: number | null };

/** Read-only Docker hosts (socket proxies) and the GitHub token used for update checks. */
export function DockerHostsCard() {
  const { data } = useUpdates();
  const { data: hosts = [] } = useHosts();
  const saveEp = useSaveDockerEndpoint();
  const delEp = useDeleteDockerEndpoint();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [test, setTest] = useState<{ ok: boolean; text: string } | null>(null);
  const [testing, setTesting] = useState(false);
  const [copied, setCopied] = useState(false);
  const eps = data?.endpoints ?? [];

  const runTest = async (url: string) => {
    setTesting(true);
    setTest(null);
    try {
      const r = await testDockerEndpoint(url);
      setTest({ ok: true, text: `Connected: ${r.containers} container${r.containers === 1 ? "" : "s"}${r.names.length ? ` (${r.names.slice(0, 6).join(", ")}${r.names.length > 6 ? ", …" : ""})` : ""}` });
    } catch (e) {
      setTest({ ok: false, text: (e as Error).message });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="grid gap-3" id="docker-hosts">
      <p className="text-xs text-muted">
        LabDash reads containers and images through a read-only <b>socket proxy</b> on each Docker host. It never gets write access to Docker.
      </p>
      <div className="overflow-hidden rounded-lg border border-line">
        {eps.length === 0 && <p className="px-4 py-4 text-center text-sm text-muted">No Docker hosts yet.</p>}
        {eps.map((e: DockerEndpoint) => (
          <div key={e.id} className="flex items-center gap-3 border-b border-line/70 px-4 py-2 text-sm last:border-0">
            <span className="font-medium">{e.name}</span>
            <span className="font-mono text-xs text-muted">{e.url}</span>
            {e.hostName && <span className="rounded bg-accent/15 px-1.5 py-px text-[11px] text-accent">{e.hostName}</span>}
            <button className="btn-ghost ml-auto px-2 py-1 text-xs" onClick={() => runTest(e.url)} disabled={testing}>
              Test
            </button>
            <button className="btn-ghost px-2 py-1 text-xs" onClick={() => setDraft({ id: e.id, name: e.name, url: e.url, hostId: e.hostId })}>
              Edit
            </button>
            <button className="btn-ghost p-1" title="Remove" onClick={() => confirm(`Remove ${e.name}?`) && delEp.mutate(e.id)}>
              <Trash2 size={13} />
            </button>
          </div>
        ))}
      </div>
      {test && !draft && <div className={test.ok ? "text-xs text-up" : "text-xs text-down"}>{test.text}</div>}

      {!draft ? (
        <div>
          <button className="btn-outline py-1.5" onClick={() => setDraft({ name: "", url: "http://", hostId: null })}>
            <Plus size={14} /> Add Docker host
          </button>
        </div>
      ) : (
        <div className="grid gap-3 rounded-lg border border-accent/40 bg-accent/5 p-4">
          <div className="grid grid-cols-[1fr_1.4fr_1fr] gap-3">
            <div>
              <label className="label">Name</label>
              <input className="input" autoFocus value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} placeholder="docker-media" />
            </div>
            <div>
              <label className="label">Socket proxy URL</label>
              <input className="input font-mono" value={draft.url} onChange={(e) => setDraft({ ...draft, url: e.target.value })} placeholder="http://192.168.8.22:2375" />
            </div>
            <div>
              <label className="label">Host (optional)</label>
              <select className="input" value={draft.hostId ?? ""} onChange={(e) => setDraft({ ...draft, hostId: e.target.value ? Number(e.target.value) : null })}>
                <option value="">—</option>
                {hosts.map((h) => (
                  <option key={h.id} value={h.id}>
                    {h.name}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <p className="text-[11px] text-faint">Linking the host lets services running on it find their containers automatically.</p>
          <div className="flex flex-wrap items-center gap-2">
            <button
              className="btn-primary py-1.5"
              disabled={!draft.name.trim() || draft.url.length < 10 || saveEp.isPending}
              onClick={() => saveEp.mutate(draft, { onSuccess: () => setDraft(null) })}
            >
              Save
            </button>
            <button className="btn-outline py-1.5" disabled={testing || draft.url.length < 10} onClick={() => runTest(draft.url)}>
              Test connection
            </button>
            <button className="btn-ghost py-1.5" onClick={() => setDraft(null)}>
              Cancel
            </button>
            {saveEp.error && <span className="text-sm text-down">{saveEp.error.message}</span>}
            {test && <span className={test.ok ? "text-xs text-up" : "text-xs text-down"}>{test.text}</span>}
          </div>
        </div>
      )}

      <details className="rounded-lg border border-line px-4 py-2 text-xs">
        <summary className="cursor-pointer text-muted">Socket proxy compose file (run on each Docker host)</summary>
        <div className="relative mt-2">
          <pre className="overflow-auto rounded-md bg-black/30 p-3 font-mono text-[11px] text-muted">{compose}</pre>
          <button
            className="btn-ghost absolute top-1.5 right-1.5 px-2 py-1 text-[11px]"
            onClick={() => {
              navigator.clipboard.writeText(compose);
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            }}
          >
            {copied ? <Check size={12} /> : <Copy size={12} />} {copied ? "Copied" : "Copy"}
          </button>
        </div>
        <p className="mt-2 text-faint">
          Only expose port 2375 on your LAN (firewall it from anything else). Then add <span className="font-mono">http://&lt;host IP&gt;:2375</span> above.
        </p>
      </details>
    </div>
  );
}

/** Optional GitHub token for more release lookups. */
export function GitHubTokenField() {
  const { data } = useUpdates();
  const save = useSaveGitHubToken();
  const [token, setToken] = useState("");
  const has = !!data?.hasGithubToken;
  return (
    <div className="grid gap-1.5">
      <div className="flex items-center gap-2">
        <input
          className="input max-w-sm font-mono"
          type="password"
          autoComplete="off"
          value={token}
          placeholder={has ? "saved — leave blank to keep" : "github_pat_… (no scopes needed)"}
          onChange={(e) => setToken(e.target.value)}
        />
        <button className="btn-outline py-1.5" disabled={!token.trim() || save.isPending} onClick={() => save.mutate(token, { onSuccess: () => setToken("") })}>
          Save
        </button>
        {has && (
          <button className="btn-ghost py-1.5 text-xs" onClick={() => save.mutate("")}>
            Remove
          </button>
        )}
      </div>
      <p className="text-[11px] text-faint">
        Optional. Without one GitHub allows 60 lookups an hour; a fine-grained token with no permissions raises that to 5,000. Stored encrypted.
      </p>
      {save.error && <span className="text-xs text-down">{save.error.message}</span>}
    </div>
  );
}
