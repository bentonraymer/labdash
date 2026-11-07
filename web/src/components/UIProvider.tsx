import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import type { Section, Service } from "@/lib/types";
import { ServiceEditor } from "./ServiceEditor";
import { SectionEditor } from "./SectionEditor";
import { Spotlight } from "./Spotlight";

interface UI {
  openSpotlight: () => void;
  editService: (s: Service | null, sectionId?: number | null) => void;
  editSection: (s: Section | null) => void;
}

const Ctx = createContext<UI | null>(null);

export function useUI(): UI {
  const v = useContext(Ctx);
  if (!v) throw new Error("useUI outside UIProvider");
  return v;
}

/** Hosts app-wide overlays (Spotlight, editors) and the global hotkeys. */
export function UIProvider({ children }: { children: ReactNode }) {
  const [spotlight, setSpotlight] = useState(false);
  const [svc, setSvc] = useState<{ open: boolean; service: Service | null; sectionId?: number | null }>({ open: false, service: null });
  const [sec, setSec] = useState<{ open: boolean; section: Section | null }>({ open: false, section: null });

  const openSpotlight = useCallback(() => setSpotlight(true), []);
  const editService = useCallback((service: Service | null, sectionId?: number | null) => setSvc({ open: true, service, sectionId }), []);
  const editSection = useCallback((section: Section | null) => setSec({ open: true, section }), []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      const typing = !!target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSpotlight((v) => !v);
      } else if (e.key === "/" && !typing && !e.metaKey && !e.ctrlKey) {
        e.preventDefault();
        setSpotlight(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const value = useMemo(() => ({ openSpotlight, editService, editSection }), [openSpotlight, editService, editSection]);

  return (
    <Ctx.Provider value={value}>
      {children}
      <Spotlight open={spotlight} onClose={() => setSpotlight(false)} />
      <ServiceEditor open={svc.open} service={svc.service} defaultSectionId={svc.sectionId} onClose={() => setSvc((p) => ({ ...p, open: false }))} />
      <SectionEditor open={sec.open} section={sec.section} onClose={() => setSec((p) => ({ ...p, open: false }))} />
    </Ctx.Provider>
  );
}
