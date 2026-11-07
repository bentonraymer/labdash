import { useEffect, useState, type FormEvent } from "react";
import { Trash2 } from "lucide-react";
import { Modal } from "./Modal";
import { ServiceIcon } from "./ServiceIcon";
import { useDeleteService, useLaunchpad, useSaveService } from "@/lib/queries";
import { slugifyIconName } from "@/lib/icons";
import type { Service } from "@/lib/types";

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
  const domain = data?.settings.dns_domain || "";

  useEffect(() => {
    if (!open) return;
    setF(service ? fromService(service) : { ...blank, sectionId: defaultSectionId ? String(defaultSectionId) : "" });
    setDnsTouched(!!service?.dnsUrl);
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
      },
      { onSuccess: onClose },
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
          <div className="glass flex size-20 shrink-0 items-center justify-center rounded-xl">
            <ServiceIcon icon={f.icon} name={f.name || "?"} className="size-11" />
          </div>
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
              <label className="label">Icon</label>
              <input
                className="input font-mono"
                value={f.icon}
                onChange={(e) => set("icon", e.target.value)}
                placeholder={`auto (di:${slugifyIconName(f.name) || "name"}) · di:<name> · https://… · emoji:🚀`}
              />
            </div>
          </div>
        </div>

        <div>
          <label className="label">Description</label>
          <input className="input" value={f.description} onChange={(e) => set("description", e.target.value)} placeholder="Media server" />
        </div>

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

        <div className="grid grid-cols-[1fr_auto] items-end gap-4">
          <div>
            <label className="label">Tags</label>
            <input className="input" value={f.tags} onChange={(e) => set("tags", e.target.value)} placeholder="media, arr" />
          </div>
          <label className="flex items-center gap-2 pb-2 text-sm text-muted">
            <input type="checkbox" checked={f.newTab} onChange={(e) => set("newTab", e.target.checked)} className="accent-accent" />
            Open in new tab
          </label>
        </div>
      </form>
    </Modal>
  );
}
