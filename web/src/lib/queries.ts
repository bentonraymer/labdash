import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { api } from "./api";
import type { Launchpad, Section, Service, Settings, SpotlightUsage, User } from "./types";

export const keys = {
  me: ["me"] as const,
  setup: ["setup"] as const,
  launchpad: ["launchpad"] as const,
  settings: ["settings"] as const,
  usage: ["spotlight-usage"] as const,
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
