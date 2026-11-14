import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { api } from "./api";
import type { Bucket, RouterSettings, RouterSnapshot, NPMSettings, NPMStatus, ProxmoxSettings, ProxmoxSnapshot, StoredBackup, Folder, Note, NoteHit, NoteSummary, NoteVersion, Device, DeviceEvent, ScanRun, ScanStatus, ScanTarget, Host, Hosting, IPCell, IPCheck, IPDetail, Incident, Subnet, Launchpad, Maintenance, Monitor, PushoverSettings, Section, Service, Settings, SpotlightUsage, User, VulnFinding, VulnScanState, VulnStatus, VulnStatusKind, VulnHistoryPoint, UpdatesResponse, UpdateSource, Reminder, ReminderKind, RepeatUnit, Todo, TodoList, PlanningSummary } from "./types";

export const keys = {
  me: ["me"] as const,
  setup: ["setup"] as const,
  launchpad: ["launchpad"] as const,
  settings: ["settings"] as const,
  usage: ["spotlight-usage"] as const,
  monitors: ["monitors"] as const,
  incidents: ["incidents"] as const,
  maintenance: ["maintenance"] as const,
  pushover: ["pushover"] as const,
  hosts: ["hosts"] as const,
  subnets: ["subnets"] as const,
  devices: ["devices"] as const,
  vulns: ["vulns"] as const,
  updates: ["updates"] as const,
  reminders: ["reminders"] as const,
  todos: ["todos"] as const,
  planning: ["planning-summary"] as const,
  vulnStatus: ["vuln-status"] as const,
  scans: ["scans"] as const,
  scanTargets: ["scan-targets"] as const,
  notes: ["notes"] as const,
  folders: ["folders"] as const,
  tags: ["tags"] as const,
  npm: ["npm-status"] as const,
  proxmox: ["proxmox"] as const,
  backups: ["backups"] as const,
};

export const useMe = () =>
  useQuery({ queryKey: keys.me, queryFn: () => api.get<User>("/api/auth/me"), retry: false, staleTime: 60_000 });

export const useSetupStatus = () =>
  useQuery({
    queryKey: keys.setup,
    queryFn: () => api.get<{ needsSetup: boolean; version: string }>("/api/setup/status"),
  });

export const useLaunchpad = () =>
  useQuery({ queryKey: keys.launchpad, queryFn: () => api.get<Launchpad>("/api/launchpad"), refetchInterval: 60_000 });

export const useSettings = () => {
  const lp = useLaunchpad();
  return lp.data?.settings;
};

export const useSpotlightUsage = () =>
  useQuery({ queryKey: keys.usage, queryFn: () => api.get<SpotlightUsage[]>("/api/spotlight/usage"), staleTime: 30_000 });

function useInvalidateLaunchpad() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: keys.launchpad });
}

export function usePatchSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: Partial<Settings>) => api.patch<Settings>("/api/settings", patch),
    onMutate: async (patch) => {
      // Optimistic so the access-mode toggle feels instant.
      await qc.cancelQueries({ queryKey: keys.launchpad });
      const prev = qc.getQueryData<Launchpad>(keys.launchpad);
      if (prev) qc.setQueryData<Launchpad>(keys.launchpad, { ...prev, settings: { ...prev.settings, ...patch } });
      return { prev };
    },
    onError: (_e, _p, ctx) => ctx?.prev && qc.setQueryData(keys.launchpad, ctx.prev),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.launchpad }),
  });
}

export type ServiceInput = Partial<Omit<Service, "id" | "url" | "directUrl" | "status" | "position" | "archivedAt" | "hosting" | "latencyMs">> & {
  clearPort?: boolean;
  clearSection?: boolean;
  clearParent?: boolean;
  archived?: boolean;
};

export function useSaveService() {
  const inv = useInvalidateLaunchpad();
  return useMutation({
    mutationFn: ({ id, ...body }: ServiceInput & { id?: number }) =>
      id ? api.patch<Service>(`/api/services/${id}`, body) : api.post<Service>("/api/services", body),
    onSuccess: inv,
  });
}

export function useDeleteService() {
  const inv = useInvalidateLaunchpad();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/services/${id}`), onSuccess: inv });
}

export function useSaveSection() {
  const inv = useInvalidateLaunchpad();
  return useMutation({
    mutationFn: ({ id, ...body }: Partial<Section> & { id?: number }) =>
      id ? api.patch(`/api/sections/${id}`, body) : api.post<{ id: number }>("/api/sections", body),
    onSuccess: inv,
  });
}

export function useDeleteSection() {
  const inv = useInvalidateLaunchpad();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/sections/${id}`), onSuccess: inv });
}

export function useSaveLayout() {
  const inv = useInvalidateLaunchpad();
  return useMutation({
    mutationFn: (sections: { id: number; serviceIds: number[] }[]) => api.put("/api/launchpad/layout", { sections }),
    onSettled: inv,
  });
}

/** Subscribes to server-sent events and keeps cached data fresh. */
export function useLiveEvents(enabled: boolean) {
  const qc = useQueryClient();
  const lastBeat = useRef(0);
  useEffect(() => {
    if (!enabled) return;
    let es: EventSource | null = null;
    let retry: number | undefined;
    const connect = () => {
      es = new EventSource("/api/events");
      es.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data) as { type: string; data?: unknown };
          if (msg.type === "launchpad" || msg.type === "status") qc.invalidateQueries({ queryKey: keys.launchpad });
          if (msg.type === "status") {
            qc.invalidateQueries({ queryKey: keys.monitors });
            qc.invalidateQueries({ queryKey: keys.incidents });
            qc.invalidateQueries({ queryKey: keys.maintenance });
          }
          if (msg.type === "scan") qc.setQueryData<{ current: ScanStatus; recent: ScanRun[] }>(keys.scans, (prev) => (prev ? { ...prev, current: msg.data as ScanStatus } : prev));
          if (msg.type === "scan" && !(msg.data as ScanStatus).running) {
            qc.invalidateQueries({ queryKey: keys.scans });
            qc.invalidateQueries({ queryKey: keys.scanTargets });
          }
          if (msg.type === "proxmox") qc.invalidateQueries({ queryKey: keys.proxmox });
          if (msg.type === "notes") {
            qc.invalidateQueries({ queryKey: keys.notes });
            qc.invalidateQueries({ queryKey: keys.tags });
            qc.invalidateQueries({ queryKey: keys.folders });
          }
          if (msg.type === "vuln") {
            qc.setQueryData<VulnStatus>(keys.vulnStatus, (prev) => (prev ? { ...prev, scan: msg.data as VulnScanState } : prev));
            if (!(msg.data as VulnScanState).running) qc.invalidateQueries({ queryKey: keys.vulnStatus });
          }
          if (msg.type === "updates") qc.invalidateQueries({ queryKey: keys.updates });
          if (msg.type === "planning") {
            qc.invalidateQueries({ queryKey: keys.reminders });
            qc.invalidateQueries({ queryKey: keys.todos });
            qc.invalidateQueries({ queryKey: keys.planning });
          }
          if (msg.type === "vulns") {
            qc.invalidateQueries({ queryKey: keys.vulns });
            qc.invalidateQueries({ queryKey: keys.vulnStatus });
            qc.invalidateQueries({ queryKey: keys.devices });
            qc.invalidateQueries({ queryKey: keys.scanTargets });
          }
          if (msg.type === "devices") {
            qc.invalidateQueries({ queryKey: keys.devices });
            qc.invalidateQueries({ queryKey: keys.subnets });
            qc.invalidateQueries({ queryKey: ["subnet-map"] });
          }
          if (msg.type === "beat" && Date.now() - lastBeat.current > 10_000) {
            // Heartbeats are frequent; refresh the status table at most every 10s.
            lastBeat.current = Date.now();
            qc.invalidateQueries({ queryKey: keys.monitors });
          }
          if (msg.type === "settings") {
            const prev = qc.getQueryData<Launchpad>(keys.launchpad);
            if (prev) qc.setQueryData<Launchpad>(keys.launchpad, { ...prev, settings: msg.data as Settings });
            qc.invalidateQueries({ queryKey: keys.launchpad });
          }
        } catch {
          /* ignore malformed */
        }
      };
      es.onerror = () => {
        es?.close();
        retry = window.setTimeout(connect, 5000);
      };
    };
    connect();
    return () => {
      es?.close();
      window.clearTimeout(retry);
    };
  }, [enabled, qc]);
}

// ── Monitoring ────────────────────────────────────────────────────────────

export const useMonitors = () =>
  useQuery({ queryKey: keys.monitors, queryFn: () => api.get<Monitor[]>("/api/monitors"), refetchInterval: 30_000 });

export const useIncidents = () =>
  useQuery({ queryKey: keys.incidents, queryFn: () => api.get<Incident[]>("/api/incidents"), refetchInterval: 60_000 });

export const useMaintenance = () =>
  useQuery({ queryKey: keys.maintenance, queryFn: () => api.get<Maintenance[]>("/api/maintenance"), refetchInterval: 60_000 });

export type HistoryRange = "24h" | "7d" | "30d";
export const useMonitorHistory = (id: number, range: HistoryRange) =>
  useQuery({
    queryKey: ["monitor-history", id, range],
    queryFn: () => api.get<{ buckets: Bucket[]; bucketSeconds: number; incidents: Incident[] }>(`/api/monitors/${id}/history?range=${range}`),
    refetchInterval: 60_000,
  });

export type MonitorInput = Partial<
  Pick<Monitor, "name" | "kind" | "target" | "intervalS" | "timeoutS" | "retries" | "expectedStatus" | "keyword" | "ignoreTls" | "enabled" | "muted" | "priority" | "sound">
> & { serviceId?: number | null; clearService?: boolean };

function useInvalidateMonitoring() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: keys.monitors });
    qc.invalidateQueries({ queryKey: keys.launchpad });
    qc.invalidateQueries({ queryKey: keys.incidents });
    qc.invalidateQueries({ queryKey: keys.maintenance });
  };
}

export function useSaveMonitor() {
  const inv = useInvalidateMonitoring();
  return useMutation({
    mutationFn: ({ id, ...body }: MonitorInput & { id?: number }) =>
      id ? api.patch(`/api/monitors/${id}`, body) : api.post<{ id: number }>("/api/monitors", body),
    onSuccess: () => {
      inv();
      // The first check runs in the background; refresh once it has likely finished.
      setTimeout(inv, 2500);
    },
  });
}

export function useDeleteMonitor() {
  const inv = useInvalidateMonitoring();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/monitors/${id}`), onSuccess: inv });
}

export function useCheckMonitor() {
  const inv = useInvalidateMonitoring();
  return useMutation({
    mutationFn: (id: number) => api.post<{ status: string; up: boolean; latencyMs: number; message: string }>(`/api/monitors/${id}/check`),
    onSuccess: inv,
  });
}

export type MaintenanceInput = Omit<Maintenance, "id" | "scopeName" | "active">;

export function useSaveMaintenance() {
  const inv = useInvalidateMonitoring();
  return useMutation({
    mutationFn: ({ id, ...body }: MaintenanceInput & { id?: number }) => (id ? api.put(`/api/maintenance/${id}`, body) : api.post("/api/maintenance", body)),
    onSuccess: inv,
  });
}

export function useDeleteMaintenance() {
  const inv = useInvalidateMonitoring();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/maintenance/${id}`), onSuccess: inv });
}

export const usePushover = () => useQuery({ queryKey: keys.pushover, queryFn: () => api.get<PushoverSettings>("/api/integrations/pushover") });

export function useSavePushover() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { enabled: boolean; device: string; baseUrl: string; userKey?: string; appToken?: string }) =>
      api.put<PushoverSettings>("/api/integrations/pushover", body),
    onSuccess: (data) => qc.setQueryData(keys.pushover, data),
  });
}

// ── Hosts, deployments, IPAM ──────────────────────────────────────────────

export const useHosts = () => useQuery({ queryKey: keys.hosts, queryFn: () => api.get<Host[]>("/api/hosts") });
export const useSubnets = () => useQuery({ queryKey: keys.subnets, queryFn: () => api.get<Subnet[]>("/api/subnets") });
export const useSubnetMap = (id: number | null) =>
  useQuery({ queryKey: ["subnet-map", id], queryFn: () => api.get<{ cells: IPCell[] }>(`/api/subnets/${id}/map`), enabled: id != null });
export const useIPDetail = (ip: string) => useQuery({ queryKey: ["ip", ip], queryFn: () => api.get<IPDetail>(`/api/ips/${ip}`) });

/** Invalidate everything derived from hosts/IPs/services. */
export function useInvalidateInventory() {
  const qc = useQueryClient();
  return () => {
    for (const k of [keys.hosts, keys.subnets, keys.launchpad, keys.monitors]) qc.invalidateQueries({ queryKey: k });
    qc.invalidateQueries({ queryKey: ["subnet-map"] });
    qc.invalidateQueries({ queryKey: ["ip"] });
  };
}

export type HostInput = Partial<Omit<Host, "id" | "nodeName" | "serviceCount">> & { clearParent?: boolean; clearVmid?: boolean };

export function useSaveHost() {
  const inv = useInvalidateInventory();
  return useMutation({
    mutationFn: ({ id, ...body }: HostInput & { id?: number }) => (id ? api.patch(`/api/hosts/${id}`, body) : api.post<{ id: number }>("/api/hosts", body)),
    onSuccess: inv,
  });
}

export function useDeleteHost() {
  const inv = useInvalidateInventory();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/hosts/${id}`), onSuccess: inv });
}

export type DeploymentInput = Pick<Hosting, "hostId" | "runtime" | "vmid" | "containerName">;

export function useSaveDeployment() {
  const inv = useInvalidateInventory();
  return useMutation({
    mutationFn: ({ serviceId, deployment }: { serviceId: number; deployment: DeploymentInput | null }) => api.put(`/api/services/${serviceId}/deployment`, deployment),
    onSuccess: inv,
  });
}

export type SubnetInput = Pick<Subnet, "cidr" | "name" | "vlan" | "gateway" | "dhcpStart" | "dhcpEnd" | "notes">;

export function useSaveSubnet() {
  const inv = useInvalidateInventory();
  return useMutation({
    mutationFn: ({ id, ...body }: SubnetInput & { id?: number }) => (id ? api.put(`/api/subnets/${id}`, body) : api.post("/api/subnets", body)),
    onSuccess: inv,
  });
}

export function useDeleteSubnet() {
  const inv = useInvalidateInventory();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/subnets/${id}`), onSuccess: inv });
}

export function useSaveAssignment() {
  const inv = useInvalidateInventory();
  return useMutation({
    mutationFn: (body: { ip: string; kind: "static" | "reserved" | "dhcp"; label: string; notes?: string; serviceId?: number | null; hostId?: number | null }) =>
      api.put("/api/ip-assignments", body),
    onSuccess: inv,
  });
}

export function useDeleteAssignment() {
  const inv = useInvalidateInventory();
  return useMutation({ mutationFn: (ip: string) => api.del(`/api/ip-assignments/${ip}`), onSuccess: inv });
}

export function useSavePortClaim() {
  const inv = useInvalidateInventory();
  return useMutation({
    mutationFn: (body: { ip: string; port: number; proto: "tcp" | "udp"; label: string; serviceId?: number | null }) => api.post("/api/port-claims", body),
    onSuccess: inv,
  });
}

export function useDeletePortClaim() {
  const inv = useInvalidateInventory();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/port-claims/${id}`), onSuccess: inv });
}

export const checkIP = (ip: string, port?: number | null, excludeService?: number) =>
  api.get<IPCheck>(`/api/ips/check?ip=${encodeURIComponent(ip)}${port ? `&port=${port}` : ""}${excludeService ? `&excludeService=${excludeService}` : ""}`);

// ── Scanning & devices ────────────────────────────────────────────────────

export const useDevices = () => useQuery({ queryKey: keys.devices, queryFn: () => api.get<Device[]>("/api/devices"), refetchInterval: 120_000 });
export const useScanTargets = () => useQuery({ queryKey: keys.scanTargets, queryFn: () => api.get<ScanTarget[]>("/api/scan-targets") });
export const useScans = () => useQuery({ queryKey: keys.scans, queryFn: () => api.get<{ current: ScanStatus; recent: ScanRun[] }>("/api/scans"), refetchInterval: 30_000 });
export const useDeviceEvents = (id: number) => useQuery({ queryKey: ["device-events", id], queryFn: () => api.get<DeviceEvent[]>(`/api/devices/${id}/events`) });

export function useSaveScanTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: Omit<ScanTarget, "id" | "lastRunAt"> & { id?: number }) =>
      id ? api.put<{ id: number }>(`/api/scan-targets/${id}`, body) : api.post<{ id: number }>("/api/scan-targets", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.scanTargets }),
  });
}

export function useDeleteScanTarget() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/scan-targets/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: keys.scanTargets }) });
}

export function useRunScan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.post<ScanStatus>(`/api/scan-targets/${id}/run`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.scans }),
  });
}

// ── Vulnerabilities ───────────────────────────────────────────────────────

export const useVulns = (status: VulnStatusKind | "all" = "open") =>
  useQuery({ queryKey: [...keys.vulns, status], queryFn: () => api.get<VulnFinding[]>(`/api/vulns?status=${status}`) });
export const useIPVulns = (ip: string) =>
  useQuery({ queryKey: [...keys.vulns, "ip", ip], queryFn: () => api.get<VulnFinding[]>(`/api/vulns?status=all&ip=${encodeURIComponent(ip)}`), enabled: !!ip });
export const useVulnHistory = (days: number) =>
  useQuery({ queryKey: [...keys.vulns, "history", days], queryFn: () => api.get<VulnHistoryPoint[]>(`/api/vulns/history?days=${days}`) });
export const useVulnStatus = (poll = false) =>
  useQuery({
    queryKey: keys.vulnStatus,
    queryFn: () => api.get<VulnStatus>("/api/vulns/status"),
    // Poll while the CVE database is syncing (that progress isn't pushed live).
    refetchInterval: (q) => (poll || q.state.data?.cveDb?.syncing ? 3000 : 60_000),
  });

export function useUpdateVuln() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: number; status?: "open" | "accepted" | "ignored"; note?: string }) => api.patch(`/api/vulns/${id}`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.vulns });
      qc.invalidateQueries({ queryKey: keys.vulnStatus });
      qc.invalidateQueries({ queryKey: keys.devices });
    },
  });
}

export function useRunVulnScan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (targetId: number) => api.post<VulnScanState>(`/api/scan-targets/${targetId}/vuln-scan`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.vulnStatus }),
  });
}

export function useVulnSync() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { full?: boolean; what?: "cve" | "templates" }) => api.post("/api/vulns/sync", body),
    onSuccess: () => setTimeout(() => qc.invalidateQueries({ queryKey: keys.vulnStatus }), 500),
  });
}

// ── Reminders & todos ─────────────────────────────────────────────────────

export const useReminders = () => useQuery({ queryKey: keys.reminders, queryFn: () => api.get<Reminder[]>("/api/reminders"), refetchInterval: 60_000 });
export const usePlanningSummary = () => useQuery({ queryKey: keys.planning, queryFn: () => api.get<PlanningSummary>("/api/planning/summary"), refetchInterval: 60_000 });

function usePlanningMutation<V>(fn: (v: V) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.reminders });
      qc.invalidateQueries({ queryKey: keys.todos });
      qc.invalidateQueries({ queryKey: keys.planning });
    },
  });
}

export type ReminderInput = { id?: number; title: string; notes: string; kind: ReminderKind; everyN: number; unit: RepeatUnit; anchorAt: string; enabled?: boolean };
export const useSaveReminder = () =>
  usePlanningMutation(({ id, ...body }: ReminderInput) => (id ? api.put(`/api/reminders/${id}`, body) : api.post<{ id: number }>("/api/reminders", body)));
export const useDeleteReminder = () => usePlanningMutation((id: number) => api.del(`/api/reminders/${id}`));
export const useReminderAction = () =>
  usePlanningMutation(({ id, action, until }: { id: number; action: "done" | "skip" | "snooze" | "test"; until?: string }) =>
    api.post(`/api/reminders/${id}/${action}`, until ? { until } : {}),
  );

export const useTodos = () => useQuery({ queryKey: keys.todos, queryFn: () => api.get<{ todos: Todo[]; lists: TodoList[] }>("/api/todos") });

export type TodoPatch = Partial<Pick<Todo, "kind" | "title" | "notes" | "priority" | "status" | "remind" | "position">> & {
  listId?: number;
  clearList?: boolean;
  parentId?: number;
  clearParent?: boolean;
  dueDate?: string;
  clearDue?: boolean;
  serviceId?: number;
  clearService?: boolean;
  hostId?: number;
  clearHost?: boolean;
};

export function useCreateTodo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: TodoPatch & { title: string }) => api.post<{ id: number }>("/api/todos", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.todos });
      qc.invalidateQueries({ queryKey: keys.planning });
    },
  });
}

/** Optimistic: the list updates immediately, the server catches up. */
export function useUpdateTodo() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: TodoPatch & { id: number }) => api.patch(`/api/todos/${id}`, body),
    onMutate: async ({ id, ...body }) => {
      await qc.cancelQueries({ queryKey: keys.todos });
      const prev = qc.getQueryData<{ todos: Todo[]; lists: TodoList[] }>(keys.todos);
      if (prev) {
        const simple: Partial<Todo> = {};
        for (const k of ["kind", "title", "notes", "priority", "status", "remind", "position"] as const) if (body[k] !== undefined) (simple as Record<string, unknown>)[k] = body[k];
        if (body.dueDate !== undefined) simple.dueDate = body.dueDate;
        if (body.clearDue) simple.dueDate = null;
        if (body.listId !== undefined) simple.listId = body.listId;
        if (body.clearList) simple.listId = null;
        qc.setQueryData(keys.todos, { ...prev, todos: prev.todos.map((t) => (t.id === id ? { ...t, ...simple } : t)) });
      }
      return { prev };
    },
    onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(keys.todos, ctx.prev),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: keys.todos });
      qc.invalidateQueries({ queryKey: keys.planning });
    },
  });
}

export const useDeleteTodo = () => usePlanningMutation((id: number) => api.del(`/api/todos/${id}`));
export const useReorderTodos = () => usePlanningMutation((ids: number[]) => api.post("/api/todos/reorder", { ids }));
export const useAddChecklist = () => usePlanningMutation(({ todoId, text }: { todoId: number; text: string }) => api.post(`/api/todos/${todoId}/checklist`, { text }));
export const useUpdateChecklist = () => usePlanningMutation(({ id, ...body }: { id: number; text?: string; done?: boolean }) => api.patch(`/api/checklist/${id}`, body));
export const useDeleteChecklist = () => usePlanningMutation((id: number) => api.del(`/api/checklist/${id}`));
export const useSaveTodoList = () =>
  usePlanningMutation(({ id, name }: { id?: number; name: string }) => (id ? api.put(`/api/todo-lists/${id}`, { name }) : api.post<{ id: number }>("/api/todo-lists", { name })));
export const useDeleteTodoList = () => usePlanningMutation((id: number) => api.del(`/api/todo-lists/${id}`));

// ── Updates ───────────────────────────────────────────────────────────────

export const useUpdates = () => useQuery({ queryKey: keys.updates, queryFn: () => api.get<UpdatesResponse>("/api/updates"), refetchInterval: 120_000 });

export function useSaveUpdateConfig() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ serviceId, ...body }: { serviceId: number; source: UpdateSource; dockerEndpointId: number | null; container: string; githubRepo: string; helperSlug: string; currentVersion: string; ignoredVersion?: string }) =>
      api.put(`/api/updates/${serviceId}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.updates }),
  });
}

export function useCheckUpdates() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (serviceId?: number) => api.post("/api/updates/check", serviceId ? { serviceId } : {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.updates }),
  });
}

export function useSaveDockerEndpoint() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id?: number; name: string; url: string; hostId: number | null }) =>
      id ? api.put(`/api/docker-endpoints/${id}`, body) : api.post("/api/docker-endpoints", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.updates }),
  });
}

export function useDeleteDockerEndpoint() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/docker-endpoints/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: keys.updates }) });
}

export const testDockerEndpoint = (url: string) => api.post<{ containers: number; names: string[] }>("/api/docker-endpoints/test", { url });

export function useSaveGitHubToken() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (token: string) => api.put("/api/updates/github-token", { token }), onSuccess: () => qc.invalidateQueries({ queryKey: keys.updates }) });
}

export function useUpdateDevice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: number; acknowledged?: boolean; ignored?: boolean; notes?: string; customName?: string; hostId?: number | null; clearHost?: boolean }) =>
      api.patch(`/api/devices/${id}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.devices }),
  });
}

export function useAcknowledgeAll() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: () => api.post("/api/devices/acknowledge-all"), onSuccess: () => qc.invalidateQueries({ queryKey: keys.devices }) });
}

export function useDeleteDevice() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/devices/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: keys.devices }) });
}

// ── Docs ──────────────────────────────────────────────────────────────────

export const useNotes = () => useQuery({ queryKey: keys.notes, queryFn: () => api.get<NoteSummary[]>("/api/notes") });
export const useFolders = () => useQuery({ queryKey: keys.folders, queryFn: () => api.get<Folder[]>("/api/folders") });
export const useTags = () => useQuery({ queryKey: keys.tags, queryFn: () => api.get<{ tag: string; count: number }[]>("/api/tags") });
export const useNote = (id: number | null) =>
  useQuery({ queryKey: ["note", id], queryFn: () => api.get<Note>(`/api/notes/${id}`), enabled: id != null, staleTime: Infinity, gcTime: 0 });
export const useNoteVersions = (id: number, enabled: boolean) =>
  useQuery({ queryKey: ["note-versions", id], queryFn: () => api.get<NoteVersion[]>(`/api/notes/${id}/versions`), enabled });
export const searchNotes = (q: string) => api.get<NoteHit[]>(`/api/notes/search?q=${encodeURIComponent(q)}`);

function useInvalidateNotes() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: keys.notes });
    qc.invalidateQueries({ queryKey: keys.folders });
    qc.invalidateQueries({ queryKey: keys.tags });
  };
}

export function useCreateNote() {
  const inv = useInvalidateNotes();
  return useMutation({
    mutationFn: (body: { title?: string; body?: string; folderId?: number | null; serviceId?: number | null; tags?: string[] }) => api.post<Note>("/api/notes", body),
    onSuccess: inv,
  });
}

export function useDeleteNote() {
  const inv = useInvalidateNotes();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/notes/${id}`), onSuccess: inv });
}

export function useSaveFolder() {
  const inv = useInvalidateNotes();
  return useMutation({
    mutationFn: ({ id, ...body }: { id?: number; name?: string; parentId?: number | null; clearParent?: boolean }) =>
      id ? api.patch(`/api/folders/${id}`, body) : api.post<{ id: number }>("/api/folders", body),
    onSuccess: inv,
  });
}

export function useDeleteFolder() {
  const inv = useInvalidateNotes();
  return useMutation({ mutationFn: (id: number) => api.del(`/api/folders/${id}`), onSuccess: inv });
}

// ── Integrations, WoL, backups ────────────────────────────────────────────

export const useNPMSettings = () => useQuery({ queryKey: ["npm-settings"], queryFn: () => api.get<NPMSettings>("/api/integrations/npm") });
export const useNPMStatus = () => useQuery({ queryKey: keys.npm, queryFn: () => api.get<NPMStatus>("/api/npm/status"), refetchInterval: 60_000, staleTime: 20_000 });
export const useProxmoxSettings = () => useQuery({ queryKey: ["proxmox-settings"], queryFn: () => api.get<ProxmoxSettings>("/api/integrations/proxmox") });
export const useProxmox = () => useQuery({ queryKey: keys.proxmox, queryFn: () => api.get<ProxmoxSnapshot>("/api/proxmox/resources"), refetchInterval: 30_000 });
export const useBackups = () => useQuery({ queryKey: keys.backups, queryFn: () => api.get<{ backups: StoredBackup[]; restorePending: boolean }>("/api/backups") });

export function useSaveIntegration<T>(kind: "npm" | "proxmox" | "glinet") {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.put<T>(`/api/integrations/${kind}`, body),
    onSuccess: (data) => {
      qc.setQueryData([`${kind}-settings`], data);
      qc.invalidateQueries({ queryKey: kind === "npm" ? keys.npm : kind === "proxmox" ? keys.proxmox : ["router"] });
    },
  });
}

export function useCreateProxyHost() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ serviceId, ...body }: { serviceId: number; certificateId: number; sslForced: boolean; websockets: boolean }) =>
      api.post<{ detail: string }>(`/api/services/${serviceId}/npm`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.npm }),
  });
}

export const wakeOnLan = (mac: string, ip?: string) => api.post<{ ok: boolean; sentTo: string[] }>("/api/wol", { mac, ip });

// ── GL.iNet router ────────────────────────────────────────────────────────

export const useRouterSettings = () =>
  useQuery({ queryKey: ["glinet-settings"], queryFn: () => api.get<RouterSettings>("/api/integrations/glinet") });
export const useRouter = () => useQuery({ queryKey: ["router"], queryFn: () => api.get<RouterSnapshot>("/api/router"), refetchInterval: 60_000 });

export function useReserveOnRouter() {
  const inv = useInvalidateInventory();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { ip: string; mac: string; name: string }) => api.post<{ detail: string }>("/api/router/reservations", body),
    onSuccess: () => {
      inv();
      setTimeout(() => qc.invalidateQueries({ queryKey: ["router"] }), 1500);
    },
  });
}
