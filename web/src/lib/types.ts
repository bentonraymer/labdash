export type AccessMode = "dns" | "direct";
export type Status = "up" | "down" | "pending" | "maintenance" | "paused" | "unknown";
export type TileSize = "sm" | "md" | "lg";

export interface User {
  id: number;
  username: string;
}

export interface Settings {
  site_title: string;
  access_mode: AccessMode;
  dns_domain: string;
  wallpaper: string;
  wallpaper_blur: number;
  wallpaper_dim: number;
  alerts_paused: boolean;
  tile_glow: boolean;
  open_new_tab: boolean;
}

export interface Section {
  id: number;
  name: string;
  icon: string;
  position: number;
  collapsed: boolean;
  tileSize: TileSize;
}

export interface QuickLink {
  label: string;
  url: string;
}

export interface Service {
  id: number;
  sectionId: number | null;
  name: string;
  icon: string;
  description: string;
  position: number;
  dnsUrl: string;
  directScheme: "http" | "https";
  directIp: string;
  directPort: number | null;
  directPath: string;
  quickLinks: QuickLink[];
  tags: string[];
  newTab: boolean;
  url: string;
  directUrl: string;
  status: Status;
  latencyMs: number | null;
  hosting: Hosting | null;
}

export interface Launchpad {
  sections: Section[];
  services: Service[];
  settings: Settings;
}

export interface SpotlightUsage {
  kind: string;
  ref: string;
  count: number;
  lastUsed: string;
}

// ── Monitoring ────────────────────────────────────────────────────────────
export type MonitorKind = "http" | "tcp" | "ping" | "dns";

export interface Bucket {
  t: string;
  up: number;
  total: number;
  maint: number;
  latency: number | null;
}

export interface Monitor {
  id: number;
  serviceId: number | null;
  serviceName: string;
  serviceIcon: string;
  sectionId: number | null;
  name: string;
  kind: MonitorKind;
  target: string;
  resolvedTarget: string;
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
  status: Status;
  lastCheckAt: string | null;
  lastChangeAt: string | null;
  lastLatencyMs: number | null;
  lastMessage: string;
  uptime24h: number | null;
  uptime7d: number | null;
  uptime30d: number | null;
  bars: Bucket[];
}

export interface Incident {
  id: number;
  monitorId: number;
  monitorName: string;
  serviceId: number | null;
  serviceIcon: string;
  startedAt: string;
  endedAt: string | null;
  cause: string;
}

export interface Maintenance {
  id: number;
  name: string;
  scope: "global" | "service" | "host";
  scopeId: number | null;
  scopeName: string;
  startsAt: string;
  endsAt: string;
  recurrence: string;
  durationMin: number;
  enabled: boolean;
  active: boolean;
}

export interface PushoverSettings {
  configured: boolean;
  enabled: boolean;
  device: string;
  baseUrl: string;
  userKeyHint: string;
  appTokenHint: string;
}

// ── Hosting & IPAM ────────────────────────────────────────────────────────
export type HostKind = "proxmox_node" | "server" | "nas" | "router" | "vm" | "lxc" | "device";
export type Runtime = "vm" | "lxc" | "docker" | "bare_metal" | "other";

export interface Host {
  id: number;
  name: string;
  kind: HostKind;
  parentId: number | null;
  vmid: number | null;
  ip: string;
  mac: string;
  os: string;
  notes: string;
  nodeName: string;
  serviceCount: number;
}

export interface Hosting {
  hostId: number | null;
  hostName: string;
  hostKind: HostKind | "";
  nodeName: string;
  runtime: Runtime;
  vmid: number | null;
  containerName: string;
}

export interface Subnet {
  id: number;
  cidr: string;
  name: string;
  vlan: number | null;
  gateway: string;
  dhcpStart: string;
  dhcpEnd: string;
  notes: string;
  used: number;
  total: number;
  nextFree: string;
}

export interface IPOwner {
  kind: "assignment" | "host" | "service" | "device";
  id: number;
  label: string;
  sub: string;
  icon?: string;
}

export type CellState = "network" | "broadcast" | "gateway" | "static" | "reserved" | "service" | "dhcp" | "discovered" | "free" | "unmanaged";

export interface IPCell {
  ip: string;
  state: CellState;
  inDhcp: boolean;
  owners: IPOwner[];
}

export interface PortRow {
  port: number;
  proto: "tcp" | "udp";
  source: "service" | "claim" | "scan";
  serviceId: number | null;
  serviceName: string;
  serviceIcon: string;
  label: string;
  claimId: number | null;
  open: boolean | null;
  banner: string;
  title: string;
  favicon: string;
  lastSeen: string;
}

export interface IPDetail {
  ip: string;
  state: CellState;
  subnet: Subnet | null;
  owners: IPOwner[];
  ports: PortRow[];
  mac: string;
}

export interface IPCheck {
  valid: boolean;
  owners?: IPOwner[];
  seenOnNetwork?: boolean;
  subnet?: string;
  inDhcp?: boolean;
  reserved?: boolean;
  portOwner?: string;
}
