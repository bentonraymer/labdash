import { useEffect, useState, type FormEvent } from "react";
import { Trash2 } from "lucide-react";
import { Modal } from "@/components/Modal";
import { useDeleteSubnet, useSaveSubnet } from "@/lib/queries";
import type { Subnet } from "@/lib/types";

export function SubnetEditor({ open, onClose, subnet }: { open: boolean; onClose: () => void; subnet: Subnet | null }) {
  const save = useSaveSubnet();
  const del = useDeleteSubnet();
  const [f, setF] = useState({ cidr: "", name: "", vlan: "", gateway: "", dhcpStart: "", dhcpEnd: "", notes: "" });
  const set = (k: keyof typeof f, v: string) => setF((p) => ({ ...p, [k]: v }));

  useEffect(() => {
    if (!open) return;
    save.reset();
    setF({
      cidr: subnet?.cidr ?? "",
      name: subnet?.name ?? "",
      vlan: subnet?.vlan != null ? String(subnet.vlan) : "",
      gateway: subnet?.gateway ?? "",
      dhcpStart: subnet?.dhcpStart ?? "",
      dhcpEnd: subnet?.dhcpEnd ?? "",
      notes: subnet?.notes ?? "",
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, subnet]);

  // Suggest the .1 gateway once a /24 is typed.
  const suggestGateway = () => {
    const m = f.cidr.match(/^(\d+\.\d+\.\d+)\.0\/24$/);
    if (m && !f.gateway) set("gateway", `${m[1]}.1`);
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate({ id: subnet?.id, ...f, vlan: f.vlan ? Number(f.vlan) : null }, { onSuccess: onClose });
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={subnet ? `Edit ${subnet.cidr}` : "Add subnet"}
      footer={
        <>
          {subnet && (
            <button type="button" className="btn-danger mr-auto" onClick={() => confirm(`Remove ${subnet.cidr}? Reservations are kept.`) && del.mutate(subnet.id, { onSuccess: onClose })}>
              <Trash2 size={15} /> Delete
            </button>
          )}
          {save.error && <span className="mr-2 text-sm text-down">{save.error.message}</span>}
          <button type="button" className="btn-ghost" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" form="subnet-form" className="btn-primary" disabled={!f.cidr.trim() || save.isPending}>
            Save
          </button>
        </>
      }
    >
      <form id="subnet-form" onSubmit={submit} className="grid gap-4">
        <div className="grid grid-cols-[1fr_1fr_90px] gap-3">
          <div>
            <label className="label">CIDR</label>
            <input className="input font-mono" autoFocus value={f.cidr} onChange={(e) => set("cidr", e.target.value)} onBlur={suggestGateway} placeholder="192.168.8.0/24" />
          </div>
          <div>
            <label className="label">Name</label>
            <input className="input" value={f.name} onChange={(e) => set("name", e.target.value)} placeholder="Servers / IoT" />
          </div>
          <div>
            <label className="label">VLAN</label>
            <input className="input font-mono" inputMode="numeric" value={f.vlan} onChange={(e) => set("vlan", e.target.value.replace(/\D/g, ""))} />
          </div>
        </div>
        <div className="grid grid-cols-3 gap-3">
          <div>
            <label className="label">Gateway</label>
            <input className="input font-mono" value={f.gateway} onChange={(e) => set("gateway", e.target.value)} placeholder="192.168.8.1" />
          </div>
          <div>
            <label className="label">DHCP start</label>
            <input className="input font-mono" value={f.dhcpStart} onChange={(e) => set("dhcpStart", e.target.value)} placeholder="192.168.8.100" />
          </div>
          <div>
            <label className="label">DHCP end</label>
            <input className="input font-mono" value={f.dhcpEnd} onChange={(e) => set("dhcpEnd", e.target.value)} placeholder="192.168.8.249" />
          </div>
        </div>
        <p className="text-xs text-faint">The DHCP range is excluded when suggesting the next free static IP. Subnets up to /22 are supported.</p>
        <div>
          <label className="label">Notes</label>
          <input className="input" value={f.notes} onChange={(e) => set("notes", e.target.value)} />
        </div>
      </form>
    </Modal>
  );
}
