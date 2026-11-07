import { Cpu, HardDrive, Laptop, type LucideIcon, Monitor, Printer, Radio, Router, Server, Smartphone, Tv, HelpCircle } from "lucide-react";
import type { Device } from "./types";

/** dashboard-icons reference for a device's OS/brand, or "" if unknown. */
export function osIcon(d: Pick<Device, "osName" | "osFamily" | "vendor" | "ports">): string {
  const os = `${d.osName} ${d.osFamily}`.toLowerCase();
  const v = d.vendor.toLowerCase();
  const has = (p: number) => d.ports.some((x) => x.port === p && x.state === "open");
  const product = d.ports.map((p) => `${p.product} ${p.title}`.toLowerCase()).join(" ");
  if (has(8006) || product.includes("proxmox")) return "di:proxmox";
  if (product.includes("truenas") || os.includes("truenas")) return "di:truenas";
  if (has(8123) || product.includes("home assistant")) return "di:home-assistant";
  if (os.includes("openwrt") || v.includes("gl technologies")) return "di:openwrt";
  if (v.includes("bambu")) return "di:bambu-lab";
  if (v.includes("raspberry")) return "di:raspberry-pi";
  if (v.includes("synology")) return "di:synology";
  if (v.includes("qnap")) return "di:qnap";
  if (v.includes("ubiquiti")) return "di:unifi";
  if (v.includes("sonos")) return "di:sonos";
  if (v.includes("espressif")) return "di:esphome";
  if (os.includes("windows") || v.includes("microsoft")) return "di:microsoft-windows";
  if (os.includes("android")) return "di:android";
  if (os.includes("ios") || os.includes("mac os") || os.includes("macos") || v.includes("apple")) return "di:apple";
  if (v.includes("google")) return "di:google-home";
  if (v.includes("amazon")) return "di:amazon";
  if (os.includes("ubuntu") || product.includes("ubuntu")) return "di:ubuntu-linux";
  if (os.includes("debian") || product.includes("debian")) return "di:debian-linux";
  if (os.includes("alpine")) return "di:alpine-linux";
  if (os.includes("linux")) return "di:linux";
  return "";
}

const typeIcons: Record<string, LucideIcon> = {
  "Proxmox VM": Monitor,
  Hypervisor: Cpu,
  Network: Router,
  Printer: Printer,
  Mobile: Smartphone,
  Media: Tv,
  IoT: Radio,
  NAS: HardDrive,
  Workstation: Laptop,
  Windows: Laptop,
  Apple: Laptop,
  Server: Server,
  Client: HelpCircle,
};

export const deviceTypeIcon = (t: string): LucideIcon => typeIcons[t] ?? HelpCircle;
