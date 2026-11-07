import { useEffect, useState, type ChangeEvent, type FormEvent } from "react";
import { Trash2 } from "lucide-react";
import { Modal } from "@/components/Modal";
import { useDeleteMonitor, useLaunchpad, useSaveMonitor } from "@/lib/queries";
import type { Monitor, MonitorKind } from "@/lib/types";

const kinds: { id: MonitorKind; label: string; hint: string }[] = [
  { id: "http", label: "HTTP(S)", hint: "Requests the URL and checks the status code (and optionally a keyword)." },
  { id: "tcp", label: "TCP port", hint: "Opens a TCP connection to host:port." },
  { id: "ping", label: "Ping", hint: "Sends one ICMP echo to the host." },
  { id: "dns", label: "DNS", hint: "Resolves a hostname. Use host@1.2.3.4 to query a specific DNS server." },
];

const sounds = ["", "pushover", "bike", "bugle", "cashregister", "classical", "cosmic", "falling", "gamelan", "incoming", "intermission", "magic", "mechanical", "pianobar", "siren", "spacealarm", "tugboat", "alien", "climb", "persistent", "echo", "updown", "vibrate", "none"];

const priorities = [
  { v: -2, label: "Lowest (no notification)" },
  { v: -1, label: "Low (quiet)" },
  { v: 0, label: "Normal" },
  { v: 1, label: "High (bypass quiet hours)" },
  { v: 2, label: "Emergency (repeat until acknowledged)" },
];

interface Form {
  serviceId: string;
  name: string;
  kind: MonitorKind;
  target: string;
  intervalS: number;
  timeoutS: number;
  retries: number;
  expectedStatus: string;
  keyword: string;
  ignoreTls: boolean;
  enabled: boolean;
  muted: boolean;
  priority: number;
  sound: string;
}

const defaults: Form = {
  serviceId: "",
  name: "",
  kind: "http",
  target: "",
  intervalS: 60,
  timeoutS: 10,
  retries: 2,
  expectedStatus: "200-399",
  keyword: "",
  ignoreTls: true,
  enabled: true,
  muted: false,
  priority: 0,
  sound: "",
};

export function MonitorEditor({ open, onClose, monitor, serviceId }: { open: boolean; onClose: () => void; monitor: Monitor | null; serviceId?: number | null }) {
  const { data } = useLaunchpad();
  const save = useSaveMonitor();
  const del = useDeleteMonitor();
  const [f, setF] = useState<Form>(defaults);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setF((p) => ({ ...p, [k]: v }));

  useEffect(() => {
    if (!open) return;
    save.reset();
    setF(
      monitor
        ? {
            serviceId: monitor.serviceId ? String(monitor.serviceId) : "",
            name: monitor.name,
            kind: monitor.kind,
            target: monitor.target,
            intervalS: monitor.intervalS,
            timeoutS: monitor.timeoutS,
            retries: monitor.retries,
            expectedStatus: monitor.expectedStatus,
            keyword: monitor.keyword,
            ignoreTls: monitor.ignoreTls,
            enabled: monitor.enabled,
            muted: monitor.muted,
            priority: monitor.priority,
            sound: monitor.sound,
          }
        : { ...defaults, serviceId: serviceId ? String(serviceId) : "" },
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, monitor, serviceId]);

  const svc = data?.services.find((s) => String(s.id) === f.serviceId);
  const derivedHint = svc
    ? f.kind === "http"
      ? svc.url || "service has no URL"
      : f.kind === "tcp"
        ? svc.directIp && svc.directPort
          ? `${svc.directIp}:${svc.directPort}`
          : "the DNS host on port 443/80"
        : f.kind === "ping"
          ? svc.directIp || "the DNS host"
          : "the DNS host"
    : "";

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(
      {
        id: monitor?.id,
        serviceId: f.serviceId ? Number(f.serviceId) : null,
        clearService: !f.serviceId,
        name: f.name,
        kind: f.kind,
        target: f.target,
        intervalS: f.intervalS,
        timeoutS: f.timeoutS,
        retries: f.retries,
        expectedStatus: f.expectedStatus,
        keyword: f.keyword,
        ignoreTls: f.ignoreTls,
        enabled: f.enabled,
        muted: f.muted,
        priority: f.priority,
        sound: f.sound,
      },
      { onSuccess: onClose },
    );
  };

  const num = (k: "intervalS" | "timeoutS" | "retries") => (e: ChangeEvent<HTMLInputElement>) => set(k, Number(e.target.value.replace(/\D/g, "")) || 0);

  return (
    <Modal
      open={open}
      onClose={onClose}
      width="max-w-2xl"
      title={monitor ? `Edit monitor · ${monitor.name || monitor.serviceName}` : "Add monitor"}
      footer={
        <>
          {monitor && (
            <button
              type="button"
              className="btn-danger mr-auto"
              onClick={() => confirm("Delete this monitor and its history?") && del.mutate(monitor.id, { onSuccess: onClose })}
            >
              <Trash2 size={15} /> Delete
            </button>
          )}
          {save.error && <span className="mr-2 text-sm text-down">{save.error.message}</span>}
          <button type="button" className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" form="monitor-form" className="btn-primary" disabled={save.isPending || (!f.serviceId && !f.target.trim())}>
            {monitor ? "Save" : "Add monitor"}
          </button>
        </>
      }
    >
      <form id="monitor-form" onSubmit={submit} className="grid gap-4">
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Service</label>
            <select className="input" value={f.serviceId} onChange={(e) => set("serviceId", e.target.value)}>
              <option value="">— Standalone (explicit target) —</option>
              {data?.services
                .slice()
                .sort((a, b) => a.name.localeCompare(b.name))
                .map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
            </select>
          </div>
          <div>
            <label className="label">Display name</label>
            <input className="input" value={f.name} onChange={(e) => set("name", e.target.value)} placeholder={svc?.name ?? "e.g. Router WAN"} />
          </div>
        </div>

        <div>
          <label className="label">Type</label>
          <div className="grid grid-cols-4 gap-2">
            {kinds.map((k) => (
              <button
                type="button"
                key={k.id}
                onClick={() => set("kind", k.id)}
                className={f.kind === k.id ? "btn border border-accent bg-accent/15 text-fg" : "btn-outline"}
              >
                {k.label}
              </button>
            ))}
          </div>
          <p className="mt-1.5 text-xs text-faint">{kinds.find((k) => k.id === f.kind)?.hint}</p>
        </div>

        <div>
          <label className="label">Target {svc && <span className="normal-case text-faint">(leave blank to use the service)</span>}</label>
          <input
            className="input font-mono"
            value={f.target}
            onChange={(e) => set("target", e.target.value)}
            placeholder={svc ? `auto → ${derivedHint}` : { http: "https://…", tcp: "192.168.8.10:22", ping: "192.168.8.1", dns: "jellyfin.home.example.com" }[f.kind]}
          />
          {svc && f.kind === "http" && !f.target && <p className="mt-1 text-xs text-faint">Follows the DNS / IP access-mode switch.</p>}
        </div>

        <div className="grid grid-cols-3 gap-3">
          <div>
            <label className="label">Interval (s)</label>
            <input className="input font-mono" inputMode="numeric" value={f.intervalS} onChange={num("intervalS")} />
          </div>
          <div>
            <label className="label">Timeout (s)</label>
            <input className="input font-mono" inputMode="numeric" value={f.timeoutS} onChange={num("timeoutS")} />
          </div>
          <div>
            <label className="label">Retries before down</label>
            <input className="input font-mono" inputMode="numeric" value={f.retries} onChange={num("retries")} />
          </div>
        </div>

        {f.kind === "http" && (
          <div className="grid grid-cols-[1fr_1fr_auto] items-end gap-3">
            <div>
              <label className="label">Accepted status codes</label>
              <input className="input font-mono" value={f.expectedStatus} onChange={(e) => set("expectedStatus", e.target.value)} placeholder="200-399,401" />
            </div>
            <div>
              <label className="label">Keyword (optional)</label>
              <input className="input" value={f.keyword} onChange={(e) => set("keyword", e.target.value)} placeholder="Text that must appear" />
            </div>
            <label className="flex items-center gap-2 pb-2 text-sm text-muted">
              <input type="checkbox" className="accent-accent" checked={f.ignoreTls} onChange={(e) => set("ignoreTls", e.target.checked)} />
              Ignore TLS errors
            </label>
          </div>
        )}

        <fieldset className="rounded-lg border border-line p-4">
          <legend className="px-1 text-xs font-semibold text-accent">Pushover alert</legend>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label">Priority</label>
              <select className="input" value={f.priority} onChange={(e) => set("priority", Number(e.target.value))}>
                {priorities.map((p) => (
                  <option key={p.v} value={p.v}>
                    {p.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="label">Sound</label>
              <select className="input" value={f.sound} onChange={(e) => set("sound", e.target.value)}>
                {sounds.map((s) => (
                  <option key={s} value={s}>
                    {s || "Device default"}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div className="mt-3 flex gap-6 text-sm text-muted">
            <label className="flex items-center gap-2">
              <input type="checkbox" className="accent-accent" checked={f.muted} onChange={(e) => set("muted", e.target.checked)} />
              Mute alerts for this monitor
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" className="accent-accent" checked={f.enabled} onChange={(e) => set("enabled", e.target.checked)} />
              Monitor enabled
            </label>
          </div>
        </fieldset>
      </form>
    </Modal>
  );
}
