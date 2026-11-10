import { useMemo } from "react";
import { useLaunchpad } from "@/lib/queries";
import type { Service } from "@/lib/types";

/** IDs of a service and everything under it (can't be its parent). */
export function descendantsOf(services: Service[], id: number): Set<number> {
  const out = new Set<number>([id]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const s of services) {
      if (s.parentId != null && out.has(s.parentId) && !out.has(s.id)) {
        out.add(s.id);
        grew = true;
      }
    }
  }
  return out;
}

/** "Runs under service" select (e.g. a container under Dockge, a site under nginx). */
export function ParentPicker({ value, onChange, selfId }: { value: string; onChange: (v: string) => void; selfId?: number }) {
  const { data } = useLaunchpad();
  const options = useMemo(() => {
    const all = data?.services ?? [];
    const blocked = selfId != null ? descendantsOf(all, selfId) : new Set<number>();
    return all.filter((s) => !blocked.has(s.id)).sort((a, b) => a.name.localeCompare(b.name));
  }, [data?.services, selfId]);
  return (
    <div>
      <label className="label">Runs under service</label>
      <select className="input" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">— Nothing (top level) —</option>
        {options.map((s) => (
          <option key={s.id} value={s.id}>
            {s.name}
          </option>
        ))}
      </select>
      <p className="mt-1 text-[11px] text-faint">e.g. a container under Dockge, or a website under its web server. Without its own hosting, it inherits the parent's.</p>
    </div>
  );
}
