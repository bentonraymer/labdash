import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import { FileUp } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { keys } from "@/lib/queries";
import { ServiceIcon } from "@/components/ServiceIcon";

interface PreviewService {
  name: string;
  icon: string;
  dnsUrl: string;
  directScheme: string;
  directIp: string;
  directPort: number | null;
  directPath: string;
  duplicate: boolean;
}
interface PreviewSection {
  name: string;
  exists: boolean;
  services: PreviewService[] | null;
}
interface Preview {
  sections: PreviewSection[];
  newServices: number;
}

/** Paste or upload a Dashy conf.yml, preview what will be created, then import. */
export function DashyImport() {
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [yaml, setYaml] = useState("");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [err, setErr] = useState("");
  const [done, setDone] = useState("");
  const [busy, setBusy] = useState(false);

  const run = async (commit: boolean) => {
    setBusy(true);
    setErr("");
    setDone("");
    try {
      if (commit) {
        const r = await api.post<{ sections: number; services: number }>("/api/import/dashy", { yaml, commit: true });
        setDone(`Imported ${r.services} services and created ${r.sections} sections.`);
        setPreview(null);
        setYaml("");
        qc.invalidateQueries({ queryKey: keys.launchpad });
      } else {
        setPreview(await api.post<Preview>("/api/import/dashy", { yaml, commit: false }));
      }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Import failed");
    } finally {
      setBusy(false);
    }
  };

  const loadFile = async (f: File) => {
    setYaml(await f.text());
    setPreview(null);
  };

  return (
    <div className="grid gap-3">
      <p className="text-xs text-muted">
        Sections are matched by name. Services that already exist (by name) are skipped, so it's safe to re-run. <code className="text-fg">hl-*</code> icons map to the dashboard-icons
        library, and links that use an IP address fill in the Direct (IP) fields.
      </p>
      <div className="flex gap-2">
        <input ref={fileRef} type="file" accept=".yml,.yaml,text/yaml" hidden onChange={(e) => e.target.files?.[0] && loadFile(e.target.files[0])} />
        <button className="btn-outline" onClick={() => fileRef.current?.click()}>
          <FileUp size={15} /> Choose conf.yml
        </button>
        <span className="self-center text-xs text-faint">or paste it below</span>
      </div>
      <textarea
        className="input h-36 font-mono text-xs"
        value={yaml}
        onChange={(e) => {
          setYaml(e.target.value);
          setPreview(null);
        }}
        placeholder={"sections:\n  - name: Media\n    items:\n      - title: Jellyfin\n        icon: hl-jellyfin\n        url: https://jellyfin.home.example.com"}
        spellCheck={false}
      />
      <div className="flex items-center gap-3">
        <button className="btn-outline" disabled={!yaml.trim() || busy} onClick={() => run(false)}>
          Preview
        </button>
        {preview && (
          <button className="btn-primary" disabled={busy || preview.newServices === 0} onClick={() => run(true)}>
            Import {preview.newServices} service{preview.newServices === 1 ? "" : "s"}
          </button>
        )}
        {err && <span className="text-sm text-down">{err}</span>}
        {done && <span className="text-sm text-up">{done}</span>}
      </div>

      {preview && (
        <div className="max-h-96 overflow-y-auto rounded-md border border-line">
          {preview.sections.map((sec) => (
            <div key={sec.name} className="border-b border-line last:border-0">
              <div className="flex items-center gap-2 bg-panel-2 px-3 py-1.5 text-xs font-semibold">
                {sec.name}
                <span className={clsx("rounded px-1.5 py-0.5 text-[10px] font-medium", sec.exists ? "bg-white/5 text-muted" : "bg-up/15 text-up")}>
                  {sec.exists ? "existing section" : "new section"}
                </span>
              </div>
              {(sec.services ?? []).map((s) => (
                <div key={s.name} className={clsx("flex items-center gap-3 px-3 py-1.5 text-[13px]", s.duplicate && "opacity-40")}>
                  <ServiceIcon icon={s.icon} name={s.name} className="size-5" />
                  <span className="w-44 truncate">{s.name}</span>
                  <span className="flex-1 truncate font-mono text-xs text-muted">
                    {s.dnsUrl || (s.directIp ? `${s.directScheme}://${s.directIp}${s.directPort ? `:${s.directPort}` : ""}${s.directPath}` : "no URL")}
                  </span>
                  {s.duplicate && <span className="text-[11px] text-muted">already exists — skipped</span>}
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
