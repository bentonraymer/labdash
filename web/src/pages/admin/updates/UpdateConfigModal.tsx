import { useState } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { Modal } from "@/components/Modal";
import { useSaveUpdateConfig } from "@/lib/queries";
import type { DockerEndpoint, UpdateItem, UpdateSource } from "@/lib/types";

const sources: { id: UpdateSource; label: string; hint: string }[] = [
  { id: "auto", label: "Automatic", hint: "Docker if the container is found, else the GitHub repo below" },
  { id: "docker", label: "Docker container", hint: "Compare the running image with its registry" },
  { id: "helper", label: "Helper Script LXC", hint: "Proxmox VE Helper Scripts app" },
  { id: "github", label: "GitHub releases", hint: "Any app with GitHub releases" },
  { id: "none", label: "Don't track", hint: "Hide from update checks" },
];

const slugify = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

/** How one service's updates are found. */
export function UpdateConfigModal({ item, endpoints, hasGithubToken, onClose }: { item: UpdateItem; endpoints: DockerEndpoint[]; hasGithubToken: boolean; onClose: () => void }) {
  const save = useSaveUpdateConfig();
  const [source, setSource] = useState<UpdateSource>(item.source);
  const [endpoint, setEndpoint] = useState<string>(item.dockerEndpointId ? String(item.dockerEndpointId) : "");
  const [container, setContainer] = useState(item.container);
  const [repo, setRepo] = useState(item.githubRepo);
  const [slug, setSlug] = useState(item.helperSlug);
  const [current, setCurrent] = useState(item.currentVersion);
  const [ignored, setIgnored] = useState(item.ignoredVersion);
  const showDocker = source === "docker" || source === "auto";
  const showRepo = source !== "none";
  const showCurrent = source === "helper" || source === "github";
  const slugShown = slug || slugify(item.name);

  const submit = () =>
    save.mutate(
      {
        serviceId: item.serviceId,
        source,
        dockerEndpointId: endpoint ? Number(endpoint) : null,
        container,
        githubRepo: repo,
        helperSlug: slug,
        currentVersion: current,
        ignoredVersion: ignored,
      },
      { onSuccess: onClose },
    );

  return (
    <Modal
      open
      onClose={onClose}
      title={`Updates for ${item.name}`}
      width="max-w-xl"
      footer={
        <>
          {save.error && <span className="mr-auto text-sm text-down">{save.error.message}</span>}
          <button className="btn-ghost py-1.5" onClick={onClose}>
            Cancel
          </button>
          <button className="btn-primary py-1.5" disabled={save.isPending} onClick={submit}>
            {save.isPending ? "Saving & checking…" : "Save & check"}
          </button>
        </>
      }
    >
      <div className="grid gap-4">
        <div className="grid gap-1.5">
          {sources.map((s) => (
            <label
              key={s.id}
              className={clsx("flex cursor-pointer items-start gap-2.5 rounded-md border px-3 py-2 text-sm", source === s.id ? "border-accent bg-accent/10" : "border-line hover:border-line-2")}
            >
              <input type="radio" className="mt-1 accent-accent" checked={source === s.id} onChange={() => setSource(s.id)} />
              <span>
                <span className="font-medium">{s.label}</span>
                <span className="block text-xs text-muted">{s.hint}</span>
              </span>
            </label>
          ))}
        </div>

        {showDocker && (
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">Docker host</label>
              <select className="input" value={endpoint} onChange={(e) => setEndpoint(e.target.value)}>
                <option value="">Find automatically</option>
                {endpoints.map((e) => (
                  <option key={e.id} value={e.id}>
                    {e.name}
                  </option>
                ))}
              </select>
              {endpoints.length === 0 && (
                <p className="mt-1 text-[11px] text-warn">
                  No Docker hosts yet: add one in <Link to="/settings#docker-hosts" className="underline">Settings</Link>.
                </p>
              )}
            </div>
            <div>
              <label className="label">Container name</label>
              <input className="input font-mono" value={container} onChange={(e) => setContainer(e.target.value)} placeholder={item.containerName || slugify(item.name)} />
              <p className="mt-1 text-[11px] text-faint">Defaults to the container set in the service's hosting.</p>
            </div>
          </div>
        )}

        {source === "helper" && (
          <div>
            <label className="label">Helper Script name</label>
            <input className="input font-mono" value={slug} onChange={(e) => setSlug(e.target.value)} placeholder={slugify(item.name)} />
            <p className="mt-1 text-[11px] text-faint">
              As in the script's URL, e.g. <span className="font-mono">homepage</span> for community-scripts.github.io/ProxmoxVE/scripts?id=homepage. LabDash reads which GitHub repo
              the script installs from.
            </p>
          </div>
        )}

        {showRepo && (
          <div>
            <label className="label">GitHub repo {source === "github" ? "" : "(optional)"}</label>
            <input className="input font-mono" value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="owner/name or a github.com link" />
            <p className="mt-1 text-[11px] text-faint">
              {source === "helper"
                ? "Only needed if the script doesn't install from GitHub releases (e.g. it uses apt)."
                : source === "github"
                  ? "LabDash compares the latest release with the installed version below."
                  : "Adds release notes, and versions for images that use tags like latest."}
              {!hasGithubToken && " Without a GitHub token, GitHub allows 60 lookups an hour (add one in Settings)."}
            </p>
          </div>
        )}

        {showCurrent && (
          <div>
            <label className="label">Installed version</label>
            <input className="input font-mono" value={current} onChange={(e) => setCurrent(e.target.value)} placeholder="e.g. 1.4.2" />
            <p className="mt-1 text-[11px] text-faint">
              {source === "helper" ? (
                <>
                  Helper Scripts record it in the LXC: run <span className="font-mono">cat /opt/{slugShown}_version.txt</span> (or check the app's About page). Update it here after you
                  upgrade.
                </>
              ) : (
                "Update it here after you upgrade."
              )}
            </p>
          </div>
        )}

        {ignored && (
          <div className="flex items-center gap-2 text-xs text-muted">
            Skipping <span className="font-mono text-fg">{ignored}</span>
            <button className="text-accent hover:underline" onClick={() => setIgnored("")}>
              Stop skipping
            </button>
          </div>
        )}
      </div>
    </Modal>
  );
}
