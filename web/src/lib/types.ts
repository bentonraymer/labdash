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
  new_device_alerts: boolean;
  backup_auto: boolean;
  alert_group_seconds: number;
  alert_max_per_hour: number;
  alert_flap_protection: boolean;
  backup_keep: number;
  tile_glow: boolean;
  open_new_tab: boolean;
  vuln_enabled: boolean;
  updates_digest: boolean;
  todo_remind_time: string;
  vuln_alert_min: "critical" | "high" | "medium" | "low" | "off";
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
  parentId: number | null;
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
  archivedAt: string | null;
}

export interface Launchpad {
  sections: Section[];
  services: Service[];
  archivedServices: Service[];
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
  serviceArchived: boolean;
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
  inherited?: boolean;
  viaService?: string;
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
  kind: "assignment" | "host" | "service" | "device" | "router";
  id: number;
  label: string;
  sub: string;
  icon?: string;
  mac?: string;
  lastSeen?: string;
  up?: boolean;
}

export type CellState = "network" | "broadcast" | "gateway" | "static" | "reserved" | "service" | "lease" | "dhcp" | "discovered" | "free" | "unmanaged";

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
  /** Another service already uses the port (a real conflict). */
  portOwner?: string;
  /** Your own port note for this IP:port, not tied to a service yet. */
  portClaim?: string;
  /** A scan found the port open: likely what you're adding. */
  portSeen?: { banner: string; title: string; lastSeen: string };
}

// ── Scanning ──────────────────────────────────────────────────────────────
export type PortProfile = "top100" | "top1000" | "full" | "custom";

export interface ScanTarget {
  id: number;
  cidr: string;
  name: string;
  schedule: string;
  portProfile: PortProfile;
  customPorts: string;
  enabled: boolean;
  lastRunAt: string | null;
  vulnEnabled?: boolean;
  vulnSchedule?: string;
  vulnLastRun?: string | null;
}

export interface ScanStatus {
  running: boolean;
  targetId: number;
  cidr: string;
  phase: string;
  percent: number;
  started: string;
  queued: number[];
}

export interface ScanRun {
  id: number;
  cidr: string;
  startedAt: string;
  finishedAt: string | null;
  status: "running" | "done" | "failed";
  hostsUp: number;
  error: string;
}

export interface DevicePort {
  port: number;
  proto: string;
  state: "open" | "closed";
  service: string;
  product: string;
  version: string;
  title: string;
  favicon: string;
  lastSeen: string;
}

export interface Device {
  id: number;
  mac: string;
  ip: string;
  hostname: string;
  customName: string;
  routerName: string;
  vendor: string;
  osName: string;
  osFamily: string;
  deviceType: string;
  isUp: boolean;
  acknowledged: boolean;
  ignored: boolean;
  notes: string;
  firstSeen: string;
  lastSeen: string;
  host: { id: number; name: string } | null;
  services: { id: number; name: string; icon?: string }[];
  ports: DevicePort[];
  /** Open vulnerability findings by severity. */
  vulns?: Partial<Record<Severity, number>>;
}

export interface DeviceEvent {
  ts: string;
  kind: string;
  detail: string;
}

// ── Docs ──────────────────────────────────────────────────────────────────
export interface Folder {
  id: number;
  parentId: number | null;
  name: string;
  position: number;
  noteCount: number;
}

export interface NoteSummary {
  id: number;
  title: string;
  folderId: number | null;
  serviceId: number | null;
  serviceName: string;
  serviceIcon: string;
  pinned: boolean;
  tags: string[];
  updatedAt: string;
  snippet: string;
}

export interface Note extends NoteSummary {
  body: string;
  createdAt: string;
}

export interface NoteVersion {
  id: number;
  title: string;
  createdAt: string;
  size: number;
}

export interface NoteHit {
  id: number;
  title: string;
  snippet: string; // matches wrapped in [[ ]]
  serviceName: string;
}

// ── Integrations ──────────────────────────────────────────────────────────
export interface NPMSettings {
  configured: boolean;
  enabled: boolean;
  url: string;
  verifyTls: boolean;
  email: string;
  hasPassword: boolean;
}

export interface NPMServiceStatus {
  state: "ok" | "mismatch" | "missing" | "none";
  proxyHostId?: number;
  forward?: string;
  detail?: string;
}

export interface NPMCert {
  id: number;
  name: string;
  domains: string[];
  expiresOn: string;
}

export interface NPMStatus {
  enabled: boolean;
  error?: string;
  services?: Record<string, NPMServiceStatus>;
  certificates?: NPMCert[];
  proxyHosts?: number;
}

export interface ProxmoxConnection {
  id: string;
  name: string;
  url: string;
  verifyTls: boolean;
  tokenId: string;
  hasSecret: boolean;
}

export interface ProxmoxSettings {
  configured: boolean;
  enabled: boolean;
  connections: ProxmoxConnection[];
}

export interface PveResource {
  id: string;
  type: "node" | "qemu" | "lxc";
  node: string;
  vmid?: number;
  name?: string;
  status: string;
  cpu: number;
  maxcpu: number;
  mem: number;
  maxmem: number;
  disk: number;
  maxdisk: number;
  uptime: number;
  connection: string;
}

export interface ProxmoxSnapshot {
  resources: PveResource[];
  fetchedAt: string;
  error: string;
  errors: { name: string; error: string }[];
  enabled: boolean;
}

export interface StoredBackup {
  name: string;
  size: number;
  createdAt: string;
}

// ── GL.iNet router ────────────────────────────────────────────────────────
export interface RouterSettings {
  configured: boolean;
  enabled: boolean;
  url: string;
  username: string;
  hasPassword: boolean;
}

export interface RouterReservation {
  name: string;
  mac: string;
  ip: string;
}

export interface RouterClient {
  mac: string;
  ip: string;
  name: string;
  alias: string;
  online: boolean;
  iface: string;
}

export interface RouterSnapshot {
  enabled: boolean;
  error: string;
  fetchedAt: string;
  reservations: RouterReservation[];
  clients: RouterClient[];
}

// ── Vulnerabilities ───────────────────────────────────────────────────────

export type Severity = "critical" | "high" | "medium" | "low" | "info";
export type VulnStatusKind = "open" | "fixed" | "accepted" | "ignored";

export interface VulnFinding {
  id: number;
  deviceId: number;
  deviceName: string;
  ip: string;
  port: number;
  source: "cve" | "nuclei" | "config";
  key: string;
  title: string;
  severity: Severity;
  cvss: number;
  kev: boolean;
  description: string;
  evidence: string;
  references: string[];
  fixedIn: string;
  confidence: "high" | "low";
  status: VulnStatusKind;
  note: string;
  firstSeen: string;
  lastSeen: string;
  resolvedAt: string | null;
}

export interface VulnScanState {
  running: boolean;
  targetId: number;
  cidr: string;
  phase: string;
  started: string;
  queued: number[];
  /** Current step (1 config & TLS, 2 CVE matching, 3 web checks) of `steps`. */
  step?: number;
  steps?: number;
  /** Percent within the current step; -1 when unknown. */
  percent?: number;
  stepStarted?: string;
  detail?: string;
  devices?: number;
  urls?: number;
  /** Findings so far in this scan. */
  found?: number;
}

export interface VulnScanRun {
  id: number;
  targetId: number | null;
  cidr: string;
  startedAt: string;
  finishedAt: string | null;
  status: "running" | "done" | "failed";
  devices: number;
  newCount: number;
  openCount: number;
  notes: string;
  error: string;
}

export interface VulnStatus {
  available: boolean;
  scan?: VulnScanState;
  cveDb?: { cves: number; kev: number; lastSync: string; lastFull: string; syncing: boolean; progress: string; error: string };
  nuclei?: { installed: boolean; version?: string; templatesUpdated: string; error: string };
  open?: Partial<Record<Severity, number>>;
  kev?: number;
  recent?: VulnScanRun[];
}

// ── Updates ───────────────────────────────────────────────────────────────

export type UpdateSource = "auto" | "docker" | "github" | "helper" | "none";
export type UpdateStatus = "current" | "outdated" | "unknown" | "error" | "untracked";

export interface UpdateItem {
  serviceId: number;
  name: string;
  icon: string;
  source: UpdateSource;
  dockerEndpointId: number | null;
  container: string;
  githubRepo: string;
  helperSlug: string;
  currentVersion: string;
  ignoredVersion: string;
  runtime: string;
  containerName: string;
  usedSource: "" | "docker" | "github" | "helper" | "none";
  status: UpdateStatus;
  runningVersion: string;
  latestVersion: string;
  image: string;
  newDigest: boolean;
  latestUrl: string;
  releaseNotes: string;
  publishedAt: string;
  resolvedRepo: string;
  detail: string;
  error: string;
  checkedAt: string | null;
}

export interface DockerEndpoint {
  id: number;
  name: string;
  url: string;
  hostId: number | null;
  hostName: string;
}

export interface NodeUpdates {
  node: string;
  connection: string;
  pveVersion: string;
  packages: { package: string; oldVersion: string; version: string; title: string; priority: string }[];
  error?: string;
}

export interface UpdatesResponse {
  items: UpdateItem[];
  endpoints: DockerEndpoint[];
  hasGithubToken: boolean;
  state?: { checking: boolean; lastRun: string; nodes: NodeUpdates[] | null; nodesAt: string };
}

export interface VulnHistoryPoint {
  day: string;
  critical: number;
  high: number;
  medium: number;
  low: number;
  info: number;
  kev: number;
  /** Rebuilt from finding dates (no snapshot existed yet). */
  estimated: boolean;
}

// ── Reminders & todos ─────────────────────────────────────────────────────

export type ReminderKind = "once" | "calendar" | "after_done";
export type RepeatUnit = "day" | "week" | "month" | "quarter" | "year";

export interface Reminder {
  id: number;
  title: string;
  notes: string;
  kind: ReminderKind;
  everyN: number;
  unit: RepeatUnit;
  anchorAt: string;
  dueAt: string;
  enabled: boolean;
  snoozedUntil: string | null;
  lastSentAt: string | null;
  lastDoneAt: string | null;
  schedule: string;
  state: "due" | "snoozed" | "upcoming" | "done" | "paused";
  events: { ts: string; kind: "done" | "skipped"; dueAt: string }[];
}

export interface ChecklistItem {
  id: number;
  text: string;
  done: boolean;
  position: number;
}

export type TodoStatus = "open" | "doing" | "done";

export interface Todo {
  id: number;
  listId: number | null;
  parentId: number | null;
  kind: "task" | "goal";
  title: string;
  notes: string;
  priority: 1 | 2 | 3 | 4;
  status: TodoStatus;
  dueDate: string | null;
  remind: boolean;
  position: number;
  serviceId: number | null;
  serviceName: string;
  serviceIcon: string;
  hostId: number | null;
  hostName: string;
  createdAt: string;
  updatedAt: string;
  completedAt: string | null;
  checklist: ChecklistItem[];
  tasksTotal: number;
  tasksDone: number;
}

export interface TodoList {
  id: number;
  name: string;
  position: number;
  open: number;
}

export interface PlanningSummary {
  remindersDue: number;
  todosOverdue: number;
  todosDueToday: number;
  openByService: Record<string, number>;
  openByHost: Record<string, number>;
}
