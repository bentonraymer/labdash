import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ImageUp, Trash2 } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { keys, useLaunchpad, usePatchSettings } from "@/lib/queries";
import { ModeToggle } from "@/components/ModeToggle";
import { PageHeader } from "./AdminLayout";
import { DashyImport } from "./DashyImport";
import { PushoverCard } from "./PushoverCard";
import { GLiNetCard, NPMCard, ProxmoxCard } from "./IntegrationCards";
import { BackupCard } from "./BackupCard";
import { DataSources } from "./network/VulnerabilitiesPage";

function Card({ title, description, children }: { title: string; description?: string; children: ReactNode }) {
  return (
    <section className="rounded-lg border border-line bg-panel">
      <div className="border-b border-line px-5 py-3">
        <h2 className="text-sm font-semibold">{title}</h2>
        {description && <p className="mt-0.5 text-xs text-muted">{description}</p>}
      </div>
      <div className="grid gap-4 p-5">{children}</div>
    </section>
  );
}

function Row({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[220px_1fr] items-center gap-4">
      <div>
        <div className="text-sm">{label}</div>
        {hint && <div className="text-xs text-faint">{hint}</div>}
      </div>
      <div>{children}</div>
    </div>
  );
}

/** Text input that saves on blur/Enter. */
function SettingText({ value, onSave, mono, placeholder }: { value: string; onSave: (v: string) => void; mono?: boolean; placeholder?: string }) {
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  return (
    <input
      className={`input max-w-sm ${mono ? "font-mono" : ""}`}
      value={v}
      placeholder={placeholder}
      onChange={(e) => setV(e.target.value)}
      onBlur={() => v !== value && onSave(v)}
      onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
    />
  );
}

export function SettingsPage() {
  const { data } = useLaunchpad();
  const patch = usePatchSettings();
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [uploadErr, setUploadErr] = useState("");
  const s = data?.settings;
  if (!s) return null;

  const upload = async (file: File) => {
    setUploadErr("");
    const fd = new FormData();
    fd.append("file", file);
    try {
      await api.post("/api/wallpaper", fd);
      qc.invalidateQueries({ queryKey: keys.launchpad });
    } catch (e) {
      setUploadErr(e instanceof ApiError ? e.message : "Upload failed");
    }
  };

  return (
    <>
      <PageHeader title="Settings" subtitle="Everything here is stored in LabDash's database — no config files needed." />
      <div className="grid max-w-4xl gap-6 p-6">
        {patch.error && <div className="rounded-md border border-down/40 bg-down/10 px-4 py-2 text-sm text-down">{patch.error.message}</div>}

        <Card title="General">
          <Row label="Site title">
            <SettingText value={s.site_title} onSave={(v) => patch.mutate({ site_title: v })} />
          </Row>
          <Row label="Internal DNS domain" hint="Used to suggest service URLs">
            <SettingText mono value={s.dns_domain} placeholder="home.example.com" onSave={(v) => patch.mutate({ dns_domain: v.trim() })} />
          </Row>
          <Row label="Access mode" hint="DNS names or direct IP:port for every link">
            <ModeToggle variant="panel" />
          </Row>
          <Row label="New-device alerts" hint="Pushover when a scan finds an unknown device">
            <Toggle checked={s.new_device_alerts} onChange={(v) => patch.mutate({ new_device_alerts: v })} />
          </Row>
          <Row label="Pause all alerts" hint="Silences Pushover notifications">
            <Toggle checked={s.alerts_paused} onChange={(v) => patch.mutate({ alerts_paused: v })} />
          </Row>
        </Card>

        <Card title="Launchpad appearance">
          <Row label="Wallpaper" hint="JPEG, PNG, WebP or AVIF up to 25 MB">
            <div className="flex items-center gap-3">
              <div
                className="h-16 w-28 rounded-md border border-line-2 bg-cover bg-center"
                style={{ backgroundImage: s.wallpaper ? `url("${s.wallpaper}")` : "linear-gradient(135deg,#1b2233,#07080c)" }}
              />
              <input ref={fileRef} type="file" accept="image/*" hidden onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
              <button className="btn-outline" onClick={() => fileRef.current?.click()}>
                <ImageUp size={15} /> Upload
              </button>
              {s.wallpaper && (
                <button className="btn-ghost" onClick={() => patch.mutate({ wallpaper: "" })}>
                  <Trash2 size={15} /> Use default
                </button>
              )}
            </div>
            {uploadErr && <p className="mt-2 text-xs text-down">{uploadErr}</p>}
          </Row>
          <Row label={`Blur · ${s.wallpaper_blur}px`}>
            <Slider value={s.wallpaper_blur} max={40} onCommit={(v) => patch.mutate({ wallpaper_blur: v })} />
          </Row>
          <Row label={`Dim · ${s.wallpaper_dim}%`}>
            <Slider value={s.wallpaper_dim} max={95} onCommit={(v) => patch.mutate({ wallpaper_dim: v })} />
          </Row>
          <Row label="Glow on down services">
            <Toggle checked={s.tile_glow} onChange={(v) => patch.mutate({ tile_glow: v })} />
          </Row>
        </Card>

        <Card title="Pushover notifications" description="Where down/up alerts are sent">
          <PushoverCard />
        </Card>

        <Card title="Nginx Proxy Manager" description="Check and create proxy hosts for your services' DNS names">
          <NPMCard />
        </Card>

        <Card title="Proxmox" description="Live CPU / RAM / disk for nodes and linked VMs & LXCs">
          <ProxmoxCard />
        </Card>

        <Card title="GL.iNet router" description="Reserved IPs and client names from your router's LAN page">
          <GLiNetCard />
        </Card>

        <Card title="Vulnerability scanning" description="Weekly safe checks: CVEs by service version, web checks (Nuclei) and weak TLS / config">
          <Row label="Enabled" hint="Scheduled scans plus daily CVE / template updates">
            <Toggle checked={s.vuln_enabled} onChange={(v) => patch.mutate({ vuln_enabled: v })} />
          </Row>
          <Row label="Alert on new findings" hint="Pushover when a scan finds something new at or above">
            <select className="input max-w-xs" value={s.vuln_alert_min} onChange={(e) => patch.mutate({ vuln_alert_min: e.target.value as typeof s.vuln_alert_min })}>
              <option value="critical">Critical only</option>
              <option value="high">High and Critical</option>
              <option value="medium">Medium and up</option>
              <option value="low">Everything (Low and up)</option>
              <option value="off">Don't alert</option>
            </select>
          </Row>
          <DataSources compact />
        </Card>

        <Card title="Backup & restore">
          <BackupCard />
        </Card>

        <Card title="Import from Dashy" description="Bring over your existing sections and tiles from a Dashy conf.yml">
          <DashyImport />
        </Card>

        <PasswordCard />
      </div>
    </>
  );
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={`relative h-5 w-9 rounded-full transition ${checked ? "bg-accent" : "bg-line-2"}`}
    >
      <span className={`absolute top-0.5 size-4 rounded-full bg-white transition-all ${checked ? "left-[18px]" : "left-0.5"}`} />
    </button>
  );
}

function Slider({ value, max, onCommit }: { value: number; max: number; onCommit: (v: number) => void }) {
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  return (
    <input
      type="range"
      min={0}
      max={max}
      value={v}
      onChange={(e) => setV(Number(e.target.value))}
      onPointerUp={() => v !== value && onCommit(v)}
      onKeyUp={() => v !== value && onCommit(v)}
      className="w-full max-w-sm accent-accent"
    />
  );
}

function PasswordCard() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await api.post("/api/auth/password", { current, next });
      setMsg({ ok: true, text: "Password updated" });
      setCurrent("");
      setNext("");
    } catch (err) {
      setMsg({ ok: false, text: err instanceof ApiError ? err.message : "Failed" });
    }
  };
  return (
    <Card title="Account">
      <form onSubmit={submit} className="grid gap-4">
        <Row label="Current password">
          <input className="input max-w-sm" type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />
        </Row>
        <Row label="New password" hint="At least 8 characters">
          <input className="input max-w-sm" type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" />
        </Row>
        <div className="flex items-center gap-3">
          <button className="btn-primary" disabled={!current || next.length < 8}>
            Change password
          </button>
          {msg && <span className={`text-sm ${msg.ok ? "text-up" : "text-down"}`}>{msg.text}</span>}
        </div>
      </form>
    </Card>
  );
}
