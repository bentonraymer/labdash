import { useEffect, useState, type FormEvent } from "react";
import { Trash2 } from "lucide-react";
import { Modal } from "@/components/Modal";
import { useDeleteHost, useHosts, useSaveHost } from "@/lib/queries";
import { hostKindLabel } from "@/lib/labels";
import type { Host, HostKind } from "@/lib/types";

const kinds = Object.keys(hostKindLabel) as HostKind[];

export function HostEditor({ open, onClose, host, parentId }: { open: boolean; onClose: () => void; host: Host | null; parentId?: number | null }) {
  const { data: hosts = [] } = useHosts();
  const save = useSaveHost();
  const del = useDeleteHost();
  const [name, setName] = useState("");
  const [kind, setKind] = useState<HostKind>("server");
  const [parent, setParent] = useState("");
  const [vmid, setVmid] = useState("");
  const [ip, setIp] = useState("");
  const [mac, setMac] = useState("");
  const [os, setOs] = useState("");
  const [notes, setNotes] = useState("");

  useEffect(() => {
    if (!open) return;
    save.reset();
    setName(host?.name ?? "");
    setKind(host?.kind ?? (parentId ? "vm" : "proxmox_node"));
    setParent(host?.parentId ? String(host.parentId) : parentId ? String(parentId) : "");
    setVmid(host?.vmid ? String(host.vmid) : "");
    setIp(host?.ip ?? "");
    setMac(host?.mac ?? "");
    setOs(host?.os ?? "");
    setNotes(host?.notes ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, host, parentId]);

  const guest = kind === "vm" || kind === "lxc";
  // A host can't be nested under itself or one of its descendants.
  const descendants = new Set<number>();
  if (host) {
    const walk = (id: number) => hosts.filter((h) => h.parentId === id).forEach((h) => (descendants.add(h.id), walk(h.id)));
    walk(host.id);
  }
  const parentOptions = hosts.filter((h) => h.id !== host?.id && !descendants.has(h.id));

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const v = vmid.trim() ? Number(vmid) : null;
    save.mutate(
      {
        id: host?.id,
        name,
        kind,
        parentId: parent ? Number(parent) : null,
        clearParent: !parent,
        vmid: v,
        clearVmid: v === null,
        ip: ip.trim(),
        mac: mac.trim(),
        os,
        notes,
      },
      { onSuccess: onClose },
    );
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={host ? `Edit ${host.name}` : "Add host"}
      footer={
        <>
          {host && (
            <button
              type="button"
              className="btn-danger mr-auto"
              onClick={() =>
                confirm(`Delete ${host.name}? Services on it keep their settings but lose their hosting link; child hosts become top-level.`) &&
                del.mutate(host.id, { onSuccess: onClose })
              }
            >
              <Trash2 size={15} /> Delete
            </button>
          )}
          {save.error && <span className="mr-2 text-sm text-down">{save.error.message}</span>}
          <button type="button" className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" form="host-form" className="btn-primary" disabled={!name.trim() || save.isPending}>
            Save
          </button>
        </>
      }
    >
      <form id="host-form" onSubmit={submit} className="grid gap-4">
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Name</label>
            <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="pve1 / docker-vm" />
          </div>
          <div>
            <label className="label">Type</label>
            <select className="input" value={kind} onChange={(e) => setKind(e.target.value as HostKind)}>
              {kinds.map((k) => (
                <option key={k} value={k}>
                  {hostKindLabel[k]}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label">Runs on</label>
            <select className="input" value={parent} onChange={(e) => setParent(e.target.value)}>
              <option value="">— Top level —</option>
              {parentOptions.map((h) => (
                <option key={h.id} value={h.id}>
                  {h.name} ({hostKindLabel[h.kind]})
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label">VMID {!guest && <span className="normal-case text-faint">(VMs/LXCs)</span>}</label>
            <input className="input font-mono" inputMode="numeric" value={vmid} disabled={!guest} onChange={(e) => setVmid(e.target.value.replace(/\D/g, ""))} placeholder="105" />
          </div>
          <div>
            <label className="label">IP address</label>
            <input className="input font-mono" value={ip} onChange={(e) => setIp(e.target.value)} placeholder="192.168.8.11" />
          </div>
          <div>
            <label className="label">MAC address</label>
            <input className="input font-mono" value={mac} onChange={(e) => setMac(e.target.value)} placeholder="bc:24:11:…  (for Wake-on-LAN)" />
          </div>
        </div>
        <div>
          <label className="label">OS</label>
          <input className="input" value={os} onChange={(e) => setOs(e.target.value)} placeholder="Proxmox VE 8.4 / Debian 12" />
        </div>
        <div>
          <label className="label">Notes</label>
          <textarea className="input h-20" value={notes} onChange={(e) => setNotes(e.target.value)} />
        </div>
      </form>
    </Modal>
  );
}
