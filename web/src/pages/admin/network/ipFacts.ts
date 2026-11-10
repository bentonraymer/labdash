import type { IPCell, IPOwner } from "@/lib/types";
import { ago } from "@/lib/time";

/** Everything known about one IP, split by where each fact comes from. */
export interface IPFacts {
  /** LabDash's own record: static / reserved / dhcp (set by you in LabDash). */
  labdash: { kind: "static" | "reserved" | "dhcp"; label: string } | null;
  /** DHCP reservation read from the GL.iNet router. */
  router: { mac: string; name: string } | null;
  /** Hosts and services LabDash knows run at this address. */
  running: IPOwner[];
  /** What the network scan / router client list last saw here. */
  seen: { up: boolean; lastSeen: string; mac: string; label: string } | null;
  /** Problems (warn) and context (info) worth a look. */
  notes: { level: "warn" | "info"; text: string }[];
  /** One-line plain-English summary for tables and tooltips. */
  summary: string;
}

const normMac = (m: string) => m.toLowerCase().replace(/-/g, ":");

export function describeIP(cell: IPCell, routerEnabled: boolean): IPFacts {
  const assignment = cell.owners.find((o) => o.kind === "assignment");
  const router = cell.owners.find((o) => o.kind === "router");
  const devices = cell.owners.filter((o) => o.kind === "device");
  const running = cell.owners.filter((o) => o.kind === "host" || o.kind === "service");
  const device = devices.sort((a, b) => (b.lastSeen ?? "").localeCompare(a.lastSeen ?? ""))[0];
  const hostMacs = cell.owners.filter((o) => o.kind === "host" && o.mac).map((o) => normMac(o.mac!));

  const f: IPFacts = {
    labdash: assignment ? { kind: assignment.sub as "static" | "reserved" | "dhcp", label: assignment.label } : null,
    router: router ? { mac: router.mac ?? router.sub, name: router.label } : null,
    running,
    seen: device ? { up: !!device.up, lastSeen: device.lastSeen ?? "", mac: device.mac ?? "", label: device.label } : null,
    notes: [],
    summary: "",
  };

  const isSpecial = cell.state === "network" || cell.state === "broadcast" || cell.state === "gateway";
  if (!isSpecial) {
    if (f.router && f.seen?.mac && normMac(f.router.mac) !== normMac(f.seen.mac)) {
      f.notes.push({ level: "warn", text: `Router reserves this IP for ${f.router.mac}, but ${f.seen.mac} is using it.` });
    }
    if (f.router && hostMacs.length && !hostMacs.includes(normMac(f.router.mac))) {
      f.notes.push({ level: "warn", text: `Router reservation MAC ${f.router.mac} doesn't match the host's MAC (${hostMacs[0]}).` });
    }
    if (cell.inDhcp && (running.length > 0 || f.labdash?.kind === "static") && !f.router) {
      f.notes.push({ level: "warn", text: "Static use inside the DHCP range without a router reservation: the router could hand this IP to another device." });
    }
    if (f.seen && running.length === 0 && !f.labdash && !f.router && !cell.inDhcp) {
      f.notes.push({ level: "warn", text: "Something is online here that nobody has claimed." });
    }
    if ((running.length > 0 || f.labdash?.kind === "static") && f.seen && !f.seen.up) {
      f.notes.push({ level: "warn", text: `Expected to be in use, but it wasn't seen in the latest scan (last seen ${ago(f.seen.lastSeen)}).` });
    }
    if (routerEnabled && running.length > 0 && !f.router && !cell.inDhcp) {
      // Fine if the device has a static IP configured on itself; just say so.
      f.notes.push({ level: "info", text: "No router reservation: fine if the device is configured with this IP itself." });
    }
  }

  const parts: string[] = [];
  if (running.length) parts.push(running.map((o) => o.label).join(", "));
  if (f.labdash) parts.push(`${f.labdash.kind} in LabDash`);
  if (f.router) parts.push(`reserved on router`);
  if (f.seen) parts.push(f.seen.up ? "online" : `last seen ${ago(f.seen.lastSeen)}`);
  if (!parts.length) parts.push(cell.state === "free" ? "free" : cell.state === "dhcp" ? "DHCP pool, unused" : cell.state);
  f.summary = parts.join(" · ");
  return f;
}
