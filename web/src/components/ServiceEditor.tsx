import { useEffect, useState, type FormEvent } from "react";
import { ImagePlus, Plus, Trash2, X } from "lucide-react";
import { Modal } from "./Modal";
import { ServiceIcon } from "./ServiceIcon";
import { IconPicker } from "./IconPicker";
import { useDeleteService, useLaunchpad, useSaveDeployment, useSaveMonitor, useSaveService } from "@/lib/queries";
import { HostingFields, hostingFromService, hostingToInput, type HostingForm } from "./HostingFields";
import { slugifyIconName } from "@/lib/icons";
import type { QuickLink, Service } from "@/lib/types";

interface Form {
  name: string;
  sectionId: string;
  icon: string;
  description: string;
  dnsUrl: string;
  directScheme: "http" | "https";
  directIp: string;
  directPort: string;
  directPath: string;
  tags: string;
  newTab: boolean;
  quickLinks: QuickLink[];
}

const blank: Form = {
  name: "",
  sectionId: "",
  icon: "",
  description: "",
  dnsUrl: "",
  directScheme: "http",
  directIp: "",
  directPort: "",
  directPath: "",
  tags: "",
  newTab: true,
  quickLinks: [],
};

function fromService(s: Service): Form {
  return {
    name: s.name,
    sectionId: s.sectionId ? String(s.sectionId) : "",
    icon: s.icon,
    description: s.description,
    dnsUrl: s.dnsUrl,
    directScheme: s.directScheme,
    directIp: s.directIp,
    directPort: s.directPort ? String(s.directPort) : "",
    directPath: s.directPath,
    tags: s.tags.join(", "),
    newTab: s.newTab,
    quickLinks: s.quickLinks.map((l) => ({ ...l })),
  };
}

export function ServiceEditor({
  open,
  onClose,
  service,
  defaultSectionId,
}: {
  open: boolean;
  onClose: () => void;
  service: Service | null;
  defaultSectionId?: number | null;
}) {
  const { data } = useLaunchpad();
  const save = useSaveService();
  const del = useDeleteService();
  const [f, setF] = useState<Form>(blank);
  const [dnsTouched, setDnsTouched] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [addMonitor, setAddMonitor] = useState(true);
  const saveMonitor = useSaveMonitor();
  const saveDeployment = useSaveDeployment();
  const [hosting, setHosting] = useState<HostingForm>(hostingFromService(null));
  const domain = data?.settings.dns_domain || "";

  useEffect(() => {
    if (!open) return;
    setF(service ? fromService(service) : { ...blank, sectionId: defaultSectionId ? String(defaultSectionId) : "" });
    setDnsTouched(!!service?.dnsUrl);
    setPickerOpen(false);
    setAddMonitor(true);
    setHosting(hostingFromService(service?.hosting));
    save.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, service, defaultSectionId]);

  const set = <K extends keyof Form>(k: K, v: Form[K]) => setF((p) => ({ ...p, [k]: v }));

  // New services: suggest https://<name>.<domain> until the user edits the DNS URL.
  useEffect(() => {
    if (service || dnsTouched || !domain) return;
    const sub = slugifyIconName(f.name);
    setF((p) => ({ ...p, dnsUrl: sub ? `https://${sub}.${domain}` : "" }));
  }, [f.name, domain, dnsTouched, service]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const port = f.directPort.trim() ? Number(f.directPort) : null;
    save.mutate(
      {
        id: service?.id,
        name: f.name,
        sectionId: f.sectionId ? Number(f.sectionId) : null,
        clearSection: !f.sectionId,
        icon: f.icon.trim(),
        description: f.description,
        dnsUrl: f.dnsUrl.trim(),
        directScheme: f.directScheme,
        directIp: f.directIp.trim(),
        directPort: port,
        clearPort: port === null,
        directPath: f.directPath.trim(),
        tags: f.tags
          .split(",")
          .map((t) => t.trim())
          .filter(Boolean),
        newTab: f.newTab,
        quickLinks: f.quickLinks.filter((l) => l.url.trim()).map((l) => ({ label: l.label.trim() || l.url.trim(), url: l.url.trim() })),
      },
      {
        onSuccess: (saved) => {
          const dep = hostingToInput(hosting);
          if (JSON.stringify(dep) !== JSON.stringify(hostingToInput(hostingFromService(service?.hosting)))) {
            saveDeployment.mutate({ serviceId: saved.id, deployment: dep });
          }
          if (!service && addMonitor && (f.dnsUrl.trim() || f.directIp.trim())) {
            saveMonitor.mutate({ serviceId: saved.id, kind: "http" });
          }
          onClose();
        },
      },
    );
  };

  const remove = () => {
    if (!service) return;
    if (!confirm(`Delete “${service.name}”? Its tile and settings will be removed.`)) return;
    del.mutate(service.id, { onSuccess: onClose });
  };

  const directPreview = f.directIp
    ? `${f.directScheme}://${f.directIp}${f.directPort ? `:${f.directPort}` : ""}${f.directPath ? (f.directPath.startsWith("/") ? f.directPath : `/${f.directPath}`) : ""}`
    : "";

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={service ? `Edit ${service.name}` : "Add service"}
      width="max-w-2xl"
      footer={
        <>
          {service && (
            <button type="button" className="btn-danger mr-auto" onClick={remove} disabled={del.isPending}>
              <Trash2 size={15} /> Delete
            </button>
          )}
          {save.error && <span className="mr-2 text-sm text-down">{save.error.message}</span>}
          <button type="button" className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" form="service-form" className="btn-primary" disabled={save.isPending || !f.name.trim()}>
            {service ? "Save" : "Add service"}
          </button>
        </>
      }
    >
      <form id="service-form" onSubmit={submit} className="grid gap-4">
        <div className="flex gap-4">
          <button
            type="button"
            onClick={() => setPickerOpen((v) => !v)}
            title="Choose icon"
            className="glass group relative flex size-20 shrink-0 items-center justify-center rounded-xl"
          >
            <ServiceIcon icon={f.icon} name={f.name || "?"} className="size-11" />
            <span className="absolute inset-0 flex items-center justify-center rounded-xl bg-black/55 opacity-0 transition group-hover:opacity-100">
              <ImagePlus size={18} />
            </span>
          </button>
          <div className="grid flex-1 grid-cols-2 gap-3">
            <div>
              <label className="label">Name</label>
              <input className="input" autoFocus value={f.name} onChange={(e) => set("name", e.target.value)} placeholder="Jellyfin" required />
            </div>
            <div>
              <label className="label">Section</label>
              <select className="input" value={f.sectionId} onChange={(e) => set("sectionId", e.target.value)}>
                <option value="">— None —</option>
                {data?.sections.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="col-span-2">
              <label className="label">Description</label>
              <input className="input" value={f.description} onChange={(e) => set("description", e.target.value)} placeholder="Media server" />
            </div>
          </div>
        </div>

        {pickerOpen && (
          <IconPicker
            value={f.icon}
            onChange={(icon) => set("icon", icon)}
            serviceName={f.name}
            serviceUrl={f.dnsUrl || directPreview}
          />
        )}

        <fieldset className="rounded-lg border border-line p-4">
          <legend className="px-1 text-xs font-semibold text-accent">DNS mode</legend>
          <label className="label">URL</label>
          <input
            className="input font-mono"
            value={f.dnsUrl}
            onChange={(e) => {
              setDnsTouched(true);
              set("dnsUrl", e.target.value);
            }}
            placeholder={`https://jellyfin.${domain || "example.com"}`}
          />
        </fieldset>

        <fieldset className="rounded-lg border border-line p-4">
          <legend className="px-1 text-xs font-semibold text-accent-2">Direct (IP) mode</legend>
          <div className="grid grid-cols-[110px_1fr_110px] gap-3">
            <div>
              <label className="label">Scheme</label>
              <select className="input" value={f.directScheme} onChange={(e) => set("directScheme", e.target.value as Form["directScheme"])}>
                <option value="http">http</option>
                <option value="https">https</option>
              </select>
            </div>
            <div>
              <label className="label">IP address</label>
              <input className="input font-mono" value={f.directIp} onChange={(e) => set("directIp", e.target.value)} placeholder="192.168.8.30" />
            </div>
            <div>
              <label className="label">Port</label>
              <input
                className="input font-mono"
                inputMode="numeric"
                value={f.directPort}
                onChange={(e) => set("directPort", e.target.value.replace(/\D/g, "").slice(0, 5))}
                placeholder="8096"
              />
            </div>
            <div className="col-span-3">
              <label className="label">Path</label>
              <input className="input font-mono" value={f.directPath} onChange={(e) => set("directPath", e.target.value)} placeholder="/web" />
            </div>
          </div>
          {directPreview && <p className="mt-2 font-mono text-xs text-muted">→ {directPreview}</p>}
        </fieldset>

        <fieldset className="rounded-lg border border-line p-4">
          <legend className="px-1 text-xs font-semibold text-muted">Hosting</legend>
          <HostingFields value={hosting} onChange={setHosting} />
        </fieldset>

        <div>
          <div className="mb-1 flex items-center justify-between">
            <span className="label mb-0">Quick links</span>
            <button
              type="button"
              className="btn-ghost px-2 py-1 text-xs"
              onClick={() => set("quickLinks", [...f.quickLinks, { label: "", url: "" }])}
            >
              <Plus size={13} /> Add link
            </button>
          </div>
          {f.quickLinks.length === 0 && <p className="text-xs text-faint">Extra links for this service (admin page, docs, Proxmox console…). They show up in Spotlight.</p>}
          <div className="grid gap-2">
            {f.quickLinks.map((l, i) => (
              <div key={i} className="grid grid-cols-[160px_1fr_auto] gap-2">
                <input
                  className="input py-1.5"
                  placeholder="Label"
                  value={l.label}
                  onChange={(e) => set("quickLinks", f.quickLinks.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))}
                />
                <input
                  className="input py-1.5 font-mono"
                  placeholder="https://… or ssh://…"
                  value={l.url}
                  onChange={(e) => set("quickLinks", f.quickLinks.map((x, j) => (j === i ? { ...x, url: e.target.value } : x)))}
                />
                <button type="button" className="btn-ghost px-2" onClick={() => set("quickLinks", f.quickLinks.filter((_, j) => j !== i))} aria-label="Remove link">
                  <X size={14} />
                </button>
              </div>
            ))}
          </div>
        </div>

        <div className="grid grid-cols-[1fr_auto] items-end gap-4">
          <div>
            <label className="label">Tags</label>
            <input className="input" value={f.tags} onChange={(e) => set("tags", e.target.value)} placeholder="media, arr" />
          </div>
          <div className="flex flex-col gap-1.5 pb-2">
          {!service && (
            <label className="flex items-center gap-2 text-sm text-muted">
              <input type="checkbox" checked={addMonitor} onChange={(e) => setAddMonitor(e.target.checked)} className="accent-accent" />
              Monitor uptime (HTTP)
            </label>
          )}
          <label className="flex items-center gap-2 text-sm text-muted">
            <input type="checkbox" checked={f.newTab} onChange={(e) => set("newTab", e.target.checked)} className="accent-accent" />
            Open in new tab
          </label>
          </div>
        </div>
      </form>
    </Modal>
  );
}
