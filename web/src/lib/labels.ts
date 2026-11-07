import { Box, Container, Cpu, HardDrive, type LucideIcon, Monitor, Router, Server, Smartphone } from "lucide-react";
import type { HostKind, Runtime } from "./types";

export const hostKindLabel: Record<HostKind, string> = {
  proxmox_node: "Proxmox node",
  server: "Server",
  nas: "NAS",
  router: "Router",
  vm: "VM",
  lxc: "LXC",
  device: "Device",
};

export const hostKindIcon: Record<HostKind, LucideIcon> = {
  proxmox_node: Cpu,
  server: Server,
  nas: HardDrive,
  router: Router,
  vm: Monitor,
  lxc: Box,
  device: Smartphone,
};

export const runtimeLabel: Record<Runtime, string> = {
  vm: "VM",
  lxc: "LXC",
  docker: "Docker",
  bare_metal: "Bare metal",
  other: "Other",
};

export const runtimeIcon: Record<Runtime, LucideIcon> = {
  vm: Monitor,
  lxc: Box,
  docker: Container,
  bare_metal: Server,
  other: Cpu,
};

/** Runtime tone for small badges (identity, not status). */
export const runtimeTone: Record<Runtime, string> = {
  vm: "bg-sky-500/15 text-sky-300",
  lxc: "bg-violet-500/15 text-violet-300",
  docker: "bg-cyan-500/15 text-cyan-300",
  bare_metal: "bg-white/8 text-muted",
  other: "bg-white/8 text-muted",
};
