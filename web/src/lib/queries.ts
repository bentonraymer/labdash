import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { api } from "./api";
import type { Bucket, Host, Hosting, IPCell, IPCheck, IPDetail, Incident, Subnet, Launchpad, Maintenance, Monitor, PushoverSettings, Section, Service, Settings, SpotlightUsage, User } from "./types";

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

export type ServiceInput = Partial<Omit<Service, "id" | "url" | "directUrl" | "status" | "position">> & {
  clearPort?: boolean;
  clearSection?: boolean;
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
