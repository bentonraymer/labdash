import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { AlertTriangle, Check, CheckCircle2, ChevronLeft, ChevronRight, ImagePlus, Info, Plus, Sparkles, X } from "lucide-react";
import { Modal } from "./Modal";
import { ServiceIcon } from "./ServiceIcon";
import { IconPicker } from "./IconPicker";
import { HostingFields, emptyHosting, hostingToInput, type HostingForm } from "./HostingFields";
import { api, ApiError } from "@/lib/api";
import { checkIP, useHosts, useLaunchpad, useNPMStatus, useSubnets } from "@/lib/queries";
import { CertPicker, defaultCert } from "./NPMPanel";
import { slugifyIconName } from "@/lib/icons";
import { runtimeLabel } from "@/lib/labels";
import type { IPCheck } from "@/lib/types";

const STEPS = ["Basics", "Hosting", "Network", "Extras", "Review"] as const;

interface ExtraPort {
  port: string;
  proto: "tcp" | "udp";
  label: string;
}

interface WizardResult {
  serviceId: number;
  steps: { step: string; ok: boolean; detail: string }[];
}

/** One form that creates a service everywhere it belongs. */
export function ServiceWizard({ open, onClose, sectionId }: { open: boolean; onClose: () => void; sectionId?: number | null }) {
  const qc = useQueryClient();
  const { data: lp } = useLaunchpad();
  const { data: subnets = [] } = useSubnets();
  const { data: hosts = [] } = useHosts();
  const domain = lp?.settings.dns_domain ?? "";
  const { data: npmStatus } = useNPMStatus();
  const npmReady = !!npmStatus?.enabled && !npmStatus.error;
  const certs = npmStatus?.certificates ?? [];
  const [npm, setNpm] = useState(true);
  const [cert, setCert] = useState(0);

  const [step, setStep] = useState(0);
  const [name, setName] = useState("");
  const [section, setSection] = useState("");
  const [icon, setIcon] = useState("");
  const [desc, setDesc] = useState("");
  const [picker, setPicker] = useState(false);
  const [hosting, setHosting] = useState<HostingForm>(emptyHosting);
  const [scheme, setScheme] = useState<"http" | "https">("http");
  const [ip, setIp] = useState("");
  const [port, setPort] = useState("");
  const [path, setPath] = useState("");
  const [sub, setSub] = useState("");
  const [subTouched, setSubTouched] = useState(false);
  const [useDns, setUseDns] = useState(true);
  const [extra, setExtra] = useState<ExtraPort[]>([]);
  const [reserveIp, setReserveIp] = useState(true);
  const [monitor, setMonitor] = useState<"" | "http" | "tcp" | "ping">("http");
  const [interval, setInterval] = useState(60);
  const [note, setNote] = useState(true);
  const [check, setCheck] = useState<IPCheck | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [result, setResult] = useState<WizardResult | null>(null);

  useEffect(() => {
    if (!open) return;
    setStep(0);
    setName("");
    setSection(sectionId ? String(sectionId) : "");
    setIcon("");
    setDesc("");
    setPicker(false);
    setHosting(emptyHosting);
    setScheme("http");
    setIp("");
    setPort("");
    setPath("");
    setSub("");
    setSubTouched(false);
    setUseDns(!!domain);
    setExtra([]);
    setReserveIp(true);
    setMonitor("http");
    setInterval(60);
    setNote(true);
    setNpm(true);
    setCheck(null);
    setErr("");
    setResult(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (!subTouched) setSub(slugifyIconName(name));
  }, [name, subTouched]);

  // Docker services usually share their host's IP: prefill it.
  useEffect(() => {
    if (ip || !hosting.hostId) return;
    const h = hosts.find((x) => String(x.id) === hosting.hostId);
    if (h?.ip && hosting.runtime === "docker") setIp(h.ip);
  }, [hosting.hostId, hosting.runtime, hosts, ip]);

  // Live IP / port conflict check.
  useEffect(() => {
    if (!ip.trim()) {
      setCheck(null);
      return;
    }
    const t = setTimeout(() => {
      checkIP(ip.trim(), port ? Number(port) : null)
        .then(setCheck)
        .catch(() => setCheck(null));
    }, 350);
    return () => clearTimeout(t);
  }, [ip, port]);

  const dnsUrl = useDns && sub && domain ? `https://${sub}.${domain}` : "";
  useEffect(() => setCert(defaultCert(certs, dnsUrl ? new URL(dnsUrl).hostname : "")), [certs, dnsUrl]);
  const canNPM = npmReady && !!dnsUrl && !!ip.trim() && !!port;

  const directPreview = ip ? `${scheme}://${ip}${port ? `:${port}` : ""}${path ? (path.startsWith("/") ? path : `/${path}`) : ""}` : "";
  const dockerOnSharedIp = hosting.runtime === "docker";
  const portTaken = !!check?.portOwner;

  const canNext = [!!name.trim(), true, !portTaken && (!!dnsUrl || !!ip.trim()) && (check?.valid ?? true) !== false, true, true][step];

  const create = async () => {
    setBusy(true);
    setErr("");
    try {
      const res = await api.post<WizardResult>("/api/services/wizard", {
        service: {
          name,
          sectionId: section ? Number(section) : null,
          icon,
          description: desc,
          dnsUrl,
          directScheme: scheme,
          directIp: ip.trim(),
          directPort: port ? Number(port) : null,
          directPath: path.trim(),
          newTab: true,
        },
        hosting: hostingToInput(hosting),
        reserveIp: reserveIp && !!ip.trim(),
        extraPorts: extra.filter((p) => p.port).map((p) => ({ port: Number(p.port), proto: p.proto, label: p.label })),
        monitor: monitor ? { kind: monitor, intervalS: interval } : null,
        createNote: note,
        npm: npm && canNPM ? { certificateId: cert, sslForced: cert > 0, websockets: true } : null,
      });
      setResult(res);
      qc.invalidateQueries();
      setTimeout(() => qc.invalidateQueries(), 2500);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Something went wrong");
    } finally {
      setBusy(false);
    }
  };

  const nextFree = useMemo(() => subnets.filter((s) => s.nextFree), [subnets]);

  if (result) {
    return (
      <Modal open={open} onClose={onClose} title={result.steps.every((s) => s.ok) ? "Service created" : "Service created — one step failed"} width="max-w-xl" footer={<button className="btn-primary" onClick={onClose}>Done</button>}>
        <div className="grid gap-2">
          {result.steps.map((s, i) => (
            <div key={i} className="flex items-start gap-3 rounded-md bg-white/[0.03] px-3 py-2">
              {s.ok ? <CheckCircle2 size={16} className="mt-0.5 shrink-0 text-up" /> : <AlertTriangle size={16} className="mt-0.5 shrink-0 text-down" />}
              <div>
                <div className="text-sm font-medium">{s.step}</div>
                <div className="text-xs text-muted">{s.detail}</div>
              </div>
            </div>
          ))}

        </div>
      </Modal>
    );
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      width="max-w-2xl"
      title={
        <div className="flex items-center gap-3">
          Add service
          <div className="flex items-center gap-1">
            {STEPS.map((s, i) => (
              <button
                key={s}
                disabled={i > step}
                onClick={() => setStep(i)}
                className={clsx(
                  "rounded-full px-2 py-0.5 text-[11px] font-medium transition",
                  i === step ? "bg-accent text-white" : i < step ? "bg-accent/20 text-accent" : "bg-white/5 text-faint",
                )}
              >
                {i < step ? <Check size={11} className="inline" /> : i + 1} {s}
              </button>
            ))}
          </div>
        </div>
      }
      footer={
        <>
          {err && <span className="mr-auto text-sm text-down">{err}</span>}
          {step > 0 && (
            <button className="btn-ghost" onClick={() => setStep(step - 1)}>
              <ChevronLeft size={15} /> Back
            </button>
          )}
          {step < STEPS.length - 1 ? (
            <button className="btn-primary" disabled={!canNext} onClick={() => setStep(step + 1)}>
              Next <ChevronRight size={15} />
            </button>
          ) : (
            <button className="btn-primary" disabled={busy} onClick={create}>
              <Sparkles size={15} /> {busy ? "Creating…" : "Create service"}
            </button>
          )}
        </>
      }
    >
      {step === 0 && (
        <div className="grid gap-4">
          <div className="flex gap-4">
            <button type="button" onClick={() => setPicker(!picker)} className="glass group relative flex size-20 shrink-0 items-center justify-center rounded-xl" title="Choose icon">
              <ServiceIcon icon={icon} name={name || "?"} className="size-11" />
              <span className="absolute inset-0 flex items-center justify-center rounded-xl bg-black/55 opacity-0 transition group-hover:opacity-100">
                <ImagePlus size={18} />
              </span>
            </button>
            <div className="grid flex-1 grid-cols-2 gap-3">
              <div>
                <label className="label">Name</label>
                <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Jellyfin" />
              </div>
              <div>
                <label className="label">Launchpad section</label>
                <select className="input" value={section} onChange={(e) => setSection(e.target.value)}>
                  <option value="">— None —</option>
                  {lp?.sections.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="col-span-2">
                <label className="label">Description</label>
                <input className="input" value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="Media server" />
              </div>
            </div>
          </div>
          {picker && <IconPicker value={icon} onChange={setIcon} serviceName={name} serviceUrl="" />}
        </div>
      )}

      {step === 1 && (
        <div className="grid gap-3">
          <p className="text-sm text-muted">Where does it run? This feeds the services table, the Hosts tree and topology. You can skip it.</p>
          <HostingFields value={hosting} onChange={setHosting} />
        </div>
      )}

      {step === 2 && (
        <div className="grid gap-4">
          <fieldset className="rounded-lg border border-line p-4">
            <legend className="px-1 text-xs font-semibold text-accent-2">Direct address</legend>
            <div className="grid grid-cols-[100px_1fr_110px] gap-3">
              <div>
                <label className="label">Scheme</label>
                <select className="input" value={scheme} onChange={(e) => setScheme(e.target.value as "http" | "https")}>
                  <option value="http">http</option>
                  <option value="https">https</option>
                </select>
              </div>
              <div>
                <label className="label">IP address</label>
                <input className="input font-mono" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="192.168.8.30" />
              </div>
              <div>
                <label className="label">Port</label>
                <input className="input font-mono" inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, "").slice(0, 5))} placeholder="8096" />
              </div>
            </div>
            {nextFree.length > 0 && !dockerOnSharedIp && (
              <div className="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-muted">
                Next free:
                {nextFree.map((s) => (
                  <button key={s.id} type="button" onClick={() => setIp(s.nextFree)} className="rounded border border-up/40 bg-up/10 px-1.5 py-0.5 font-mono text-up">
                    {s.nextFree} <span className="text-muted">{s.name || s.cidr}</span>
                  </button>
                ))}
              </div>
            )}
            <div className="mt-3">
              <label className="label">Path</label>
              <input className="input font-mono" value={path} onChange={(e) => setPath(e.target.value)} placeholder="/web (optional)" />
            </div>
            <IPFeedback check={check} ip={ip} port={port} sharedIp={dockerOnSharedIp} />
          </fieldset>

          <fieldset className="rounded-lg border border-line p-4">
            <legend className="px-1 text-xs font-semibold text-accent">DNS name</legend>
            {domain ? (
              <>
                <label className="flex items-center gap-2 text-sm text-muted">
                  <input type="checkbox" className="accent-accent" checked={useDns} onChange={(e) => setUseDns(e.target.checked)} />
                  Reachable through the wildcard *.{domain}
                </label>
                {useDns && (
                  <div className="mt-2 flex items-center gap-1 font-mono text-sm">
                    <span className="text-muted">https://</span>
                    <input
                      className="input w-48 py-1.5"
                      value={sub}
                      onChange={(e) => {
                        setSubTouched(true);
                        setSub(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ""));
                      }}
                    />
                    <span className="text-muted">.{domain}</span>
                  </div>
                )}
              </>
            ) : (
              <p className="text-xs text-muted">Set your internal DNS domain in Settings to get DNS URLs suggested here.</p>
            )}
          </fieldset>

          <div>
            <div className="mb-1 flex items-center justify-between">
              <span className="label mb-0">Other ports this service uses</span>
              <button type="button" className="btn-ghost px-2 py-1 text-xs" onClick={() => setExtra([...extra, { port: "", proto: "tcp", label: "" }])} disabled={!ip}>
                <Plus size={13} /> Add port
              </button>
            </div>
            {extra.map((p, i) => (
              <div key={i} className="mb-2 grid grid-cols-[100px_80px_1fr_auto] gap-2">
                <input className="input py-1.5 font-mono" placeholder="8920" value={p.port} onChange={(e) => setExtra(extra.map((x, j) => (j === i ? { ...x, port: e.target.value.replace(/\D/g, "").slice(0, 5) } : x)))} />
                <select className="input py-1.5" value={p.proto} onChange={(e) => setExtra(extra.map((x, j) => (j === i ? { ...x, proto: e.target.value as "tcp" | "udp" } : x)))}>
                  <option value="tcp">tcp</option>
                  <option value="udp">udp</option>
                </select>
                <input className="input py-1.5" placeholder="HTTPS / discovery…" value={p.label} onChange={(e) => setExtra(extra.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))} />
                <button type="button" className="btn-ghost px-2" onClick={() => setExtra(extra.filter((_, j) => j !== i))}>
                  <X size={14} />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {step === 3 && (
        <div className="grid gap-3">
          <Option checked={!!monitor} onChange={(v) => setMonitor(v ? "http" : "")} title="Uptime monitor" desc="Alerts via Pushover when it goes down.">
            {monitor && (
              <div className="mt-2 flex items-center gap-2">
                {(["http", "tcp", "ping"] as const).map((k) => (
                  <button key={k} type="button" onClick={() => setMonitor(k)} className={clsx("rounded-md border px-2.5 py-1 text-xs uppercase", monitor === k ? "border-accent bg-accent/15" : "border-line-2 text-muted")}>
                    {k}
                  </button>
                ))}
                <span className="ml-2 text-xs text-muted">every</span>
                <select className="input w-24 py-1 text-xs" value={interval} onChange={(e) => setInterval(Number(e.target.value))}>
                  {[30, 60, 120, 300].map((s) => (
                    <option key={s} value={s}>
                      {s < 60 ? `${s}s` : `${s / 60} min`}
                    </option>
                  ))}
                </select>
              </div>
            )}
          </Option>
          <Option checked={reserveIp && !!ip} disabled={!ip || dockerOnSharedIp} onChange={setReserveIp} title="Mark the IP as static" desc={dockerOnSharedIp ? "Docker services share their host's IP, so it's not reserved separately." : ip ? `Records ${ip} as a static assignment in IP Addresses.` : "No IP entered."} />
          <Option checked={note} onChange={setNote} title="Create a docs page" desc="A markdown page pre-filled with access details and hosting, ready for config notes." />
          <Option
            checked={npm && canNPM}
            disabled={!canNPM}
            onChange={setNpm}
            title="Create Nginx Proxy Manager host"
            desc={
              !npmReady
                ? npmStatus?.error
                  ? `NPM error: ${npmStatus.error}`
                  : "Connect NPM in Settings to enable this."
                : !dnsUrl || !ip.trim() || !port
                  ? "Needs a DNS name plus a direct IP and port to forward to."
                  : `${dnsUrl.replace("https://", "")} → ${scheme}://${ip}:${port}`
            }
          >
            {npm && canNPM && (
              <div className="mt-2 flex items-center gap-2 text-xs text-muted">
                SSL certificate <CertPicker certs={certs} domain={dnsUrl ? new URL(dnsUrl).hostname : ""} value={cert} onChange={setCert} />
              </div>
            )}
          </Option>
        </div>
      )}

      {step === 4 && (
        <div className="grid gap-3 text-sm">
          <div className="flex items-center gap-3">
            <ServiceIcon icon={icon} name={name} className="size-10" />
            <div>
              <div className="text-base font-semibold">{name}</div>
              <div className="text-xs text-muted">{desc || "No description"}</div>
            </div>
          </div>
          <Review label="Section" value={lp?.sections.find((s) => String(s.id) === section)?.name ?? "None"} />
          <Review
            label="Hosting"
            value={
              hosting.runtime
                ? `${runtimeLabel[hosting.runtime]}${hosting.hostId ? ` on ${hosts.find((h) => String(h.id) === hosting.hostId)?.name}` : ""}${hosting.vmid ? ` · VMID ${hosting.vmid}` : ""}${hosting.containerName ? ` · ${hosting.containerName}` : ""}`
                : "Not set"
            }
          />
          <Review label="DNS URL" value={dnsUrl || "—"} mono />
          <Review label="Direct URL" value={directPreview || "—"} mono />
          {extra.filter((p) => p.port).length > 0 && <Review label="Extra ports" value={extra.filter((p) => p.port).map((p) => `${p.port}/${p.proto}${p.label ? ` ${p.label}` : ""}`).join(", ")} mono />}
          <Review label="Monitor" value={monitor ? `${monitor.toUpperCase()} every ${interval}s` : "None"} />
          <Review label="IP reservation" value={reserveIp && ip && !dockerOnSharedIp ? `${ip} → static` : "No"} />
          <Review label="Docs page" value={note ? "Yes" : "No"} />
          <Review label="NPM proxy host" value={npm && canNPM ? `Yes${cert ? ` · ${certs.find((c) => c.id === cert)?.name ?? "certificate"}` : " · HTTP only"}` : "No"} />
        </div>
      )}
    </Modal>
  );
}

function IPFeedback({ check, ip, port, sharedIp }: { check: IPCheck | null; ip: string; port: string; sharedIp: boolean }) {
  if (!ip.trim() || !check) return null;
  if (!check.valid) return <Note tone="down" icon={<AlertTriangle size={13} />}>Not a valid IP address.</Note>;
  const others = (check.owners ?? []).filter((o) => o.kind !== "device");
  return (
    <div className="mt-3 grid gap-1.5">
      {check.subnet ? <Note tone="muted" icon={<Info size={13} />}>In {check.subnet}</Note> : <Note tone="warn" icon={<Info size={13} />}>Not inside any subnet you've added.</Note>}
      {check.reserved && <Note tone="down" icon={<AlertTriangle size={13} />}>That's the network, broadcast or gateway address.</Note>}
      {check.inDhcp && <Note tone="warn" icon={<AlertTriangle size={13} />}>Inside the DHCP range: the router could hand it to another device.</Note>}
      {others.length > 0 &&
        (sharedIp ? (
          <Note tone="muted" icon={<Info size={13} />}>Shared with {others.map((o) => o.label).join(", ")} (fine for Docker).</Note>
        ) : (
          <Note tone="warn" icon={<AlertTriangle size={13} />}>Already used by {others.map((o) => `${o.label} (${o.kind})`).join(", ")}.</Note>
        ))}
      {check.seenOnNetwork && others.length === 0 && <Note tone="warn" icon={<AlertTriangle size={13} />}>A device was recently seen at this IP by a scan.</Note>}
      {port && check.portOwner && <Note tone="down" icon={<AlertTriangle size={13} />}>Port {port} is already used by {check.portOwner}.</Note>}
      {port && !check.portOwner && <Note tone="up" icon={<Check size={13} />}>Port {port} is free on this IP.</Note>}
    </div>
  );
}

function Note({ tone, icon, children }: { tone: "up" | "down" | "warn" | "muted"; icon: ReactNode; children: ReactNode }) {
  const c = { up: "text-up", down: "text-down", warn: "text-warn", muted: "text-muted" }[tone];
  return (
    <div className={clsx("flex items-center gap-1.5 text-xs", c)}>
      {icon}
      <span>{children}</span>
    </div>
  );
}

function Option({ checked, onChange, title, desc, disabled, children }: { checked: boolean; onChange: (v: boolean) => void; title: string; desc: string; disabled?: boolean; children?: ReactNode }) {
  return (
    <div className={clsx("rounded-lg border p-3 transition", checked ? "border-accent/60 bg-accent/5" : "border-line", disabled && "opacity-50")}>
      <label className={clsx("flex items-start gap-3", disabled ? "cursor-not-allowed" : "cursor-pointer")}>
        <input type="checkbox" className="mt-1 accent-accent" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
        <div>
          <div className="text-sm font-medium">{title}</div>
          <div className="text-xs text-muted">{desc}</div>
        </div>
      </label>
      {children && <div className="pl-7">{children}</div>}
    </div>
  );
}

function Review({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[140px_1fr] gap-3 border-b border-line/60 pb-2">
      <span className="text-muted">{label}</span>
      <span className={clsx("break-all", mono && "font-mono text-xs")}>{value}</span>
    </div>
  );
}
