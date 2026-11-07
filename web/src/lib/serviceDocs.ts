import { useCallback } from "react";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import { keys } from "./queries";
import type { Note, NoteSummary } from "./types";

/** Opens a service's docs page, creating it on first use. */
export function useOpenServiceDocs() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  return useCallback(
    async (serviceId: number, name: string) => {
      const notes = await qc.fetchQuery({ queryKey: keys.notes, queryFn: () => api.get<NoteSummary[]>("/api/notes") });
      const existing = notes.find((n) => n.serviceId === serviceId);
      if (existing) return navigate(`/docs/${existing.id}`);
      const n = await api.post<Note>("/api/notes", { title: name, serviceId, body: `# ${name}\n\n## Configuration\n\n## Notes\n` });
      qc.invalidateQueries({ queryKey: keys.notes });
      navigate(`/docs/${n.id}`);
    },
    [navigate, qc],
  );
}
