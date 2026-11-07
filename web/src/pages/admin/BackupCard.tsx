import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Download, HardDriveDownload, RotateCcw, Trash2, Upload } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { keys, useBackups, useLaunchpad, usePatchSettings } from "@/lib/queries";
import { ago, fmtDateTime } from "@/lib/time";

const fmtSize = (b: number) => (b > 1 << 20 ? `${(b / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(b / 1024))} KB`);

/** Download, schedule and restore backups of the whole data directory. */
export function BackupCard() {
  const qc = useQueryClient();
  const { data } = useBackups();
  const { data: lp } = useLaunchpad();
  const patch = usePatchSettings();
  const fileRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const s = lp?.settings;

  const createNow = async () => {
    setBusy(true);
    setMsg(null);
    try {
      const r = await api.post<{ name: string }>("/api/backups");
      setMsg({ ok: true, text: `Saved ${r.name}` });
      qc.invalidateQueries({ queryKey: keys.backups });
    } catch (e) {
      setMsg({ ok: false, text: e instanceof ApiError ? e.message : "Failed" });
    } finally {
      setBusy(false);
    }
  };

  const restore = async (opts: { file?: File; name?: string }) => {
    const label = opts.file?.name ?? opts.name;
    if (!confirm(`Restore from ${label}?\n\nEverything in LabDash will be replaced with the backup's contents. The current database is kept in /data/backups as pre-restore-*.db. LabDash restarts to finish.`)) return;
    setBusy(true);
    setMsg(null);
    try {
      let r: { restarting: boolean };
      if (opts.file) {
        const fd = new FormData();
        fd.append("file", opts.file);
        r = await api.post("/api/restore", fd);
      } else {
        r = await api.post(`/api/restore?name=${encodeURIComponent(opts.name!)}`);
      }
      setMsg({ ok: true, text: r.restarting ? "Restore staged — LabDash is restarting…" : "Restore staged — restart LabDash to apply it." });
      if (r.restarting) waitForRestart();
    } catch (e) {
      setMsg({ ok: false, text: e instanceof ApiError ? e.message : "Restore failed" });
      setBusy(false);
    }
  };

  const waitForRestart = () => {
    const started = Date.now();
    const poll = async () => {
      try {
        await new Promise((r) => setTimeout(r, 2000));
        const res = await fetch("/healthz", { cache: "no-store" });
        if (res.ok && Date.now() - started > 3000) return window.location.reload();
      } catch {
        /* still down */
      }
      if (Date.now() - started < 120_000) poll();
      else setMsg({ ok: false, text: "LabDash hasn't come back. Check that the container has a restart policy (restart: unless-stopped)." });
    };
    poll();
  };

  const remove = async (name: string) => {
    if (!confirm(`Delete ${name}?`)) return;
    await api.del(`/api/backups/${encodeURIComponent(name)}`);
    qc.invalidateQueries({ queryKey: keys.backups });
  };

  return (
    <div className="grid gap-4">
      <p className="text-xs text-muted">
        A backup is a zip of the database (all services, monitors, notes, history and encrypted credentials) plus uploads, custom icons and wallpapers. Keep a copy off the
        LabDash host too.
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <a className="btn-primary" href="/api/backup" download>
          <Download size={14} /> Download backup
        </a>
        <button className="btn-outline" onClick={createNow} disabled={busy}>
          <HardDriveDownload size={14} /> Save to /data/backups
        </button>
        <input ref={fileRef} type="file" accept=".zip,application/zip" hidden onChange={(e) => e.target.files?.[0] && restore({ file: e.target.files[0] })} />
        <button className="btn-outline" onClick={() => fileRef.current?.click()} disabled={busy}>
          <Upload size={14} /> Restore from file…
        </button>
        {msg && <span className={`text-sm ${msg.ok ? "text-up" : "text-down"}`}>{msg.text}</span>}
      </div>
      {s && (
        <div className="flex flex-wrap items-center gap-4 text-sm text-muted">
          <label className="flex items-center gap-2">
            <input type="checkbox" className="accent-accent" checked={s.backup_auto} onChange={(e) => patch.mutate({ backup_auto: e.target.checked })} />
            Nightly backup at 03:30
          </label>
          <label className="flex items-center gap-2">
            keep last
            <select className="input w-20 py-1" value={s.backup_keep} onChange={(e) => patch.mutate({ backup_keep: Number(e.target.value) })}>
              {[3, 7, 14, 30].map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
      {data && data.backups.length > 0 && (
        <div className="overflow-hidden rounded-md border border-line">
          {data.backups.map((b) => (
            <div key={b.name} className="flex items-center gap-3 border-b border-line/70 px-3 py-2 text-sm last:border-0">
              <span className="font-mono text-xs">{b.name}</span>
              <span className="text-xs text-faint" title={fmtDateTime(b.createdAt)}>
                {ago(b.createdAt)} · {fmtSize(b.size)}
              </span>
              <span className="ml-auto flex gap-1">
                <a className="btn-ghost p-1.5" title="Download" href={`/api/backups/${encodeURIComponent(b.name)}`} download>
                  <Download size={13} />
                </a>
                <button className="btn-ghost p-1.5" title="Restore" disabled={busy} onClick={() => restore({ name: b.name })}>
                  <RotateCcw size={13} />
                </button>
                <button className="btn-ghost p-1.5 hover:text-down" title="Delete" onClick={() => remove(b.name)}>
                  <Trash2 size={13} />
                </button>
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
