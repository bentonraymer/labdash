import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { Globe, Link2, Search, Sparkles, Upload } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { fuzzyMatch } from "@/lib/fuzzy";
import { slugifyIconName } from "@/lib/icons";

interface IconEntry {
  n: string; // name
  f: "svg" | "png" | "webp";
  a?: string[]; // aliases
  c?: string[]; // categories
}

type Tab = "library" | "upload" | "site" | "custom";

const useIconIndex = (enabled: boolean) =>
  useQuery({
    queryKey: ["icon-index"],
    queryFn: () => api.get<IconEntry[]>("/api/icons/index"),
    staleTime: Infinity,
    gcTime: Infinity,
    enabled,
  });

/**
 * Inline icon chooser: search the dashboard-icons library, upload an image,
 * grab the service's own favicon, or enter a URL/emoji.
 */
export function IconPicker({
  value,
  onChange,
  serviceName,
  serviceUrl,
}: {
  value: string;
  onChange: (icon: string) => void;
  serviceName: string;
  serviceUrl: string;
}) {
  const [tab, setTab] = useState<Tab>("library");
  const [q, setQ] = useState("");
  const index = useIconIndex(tab === "library");
  const query = q.trim() || serviceName.trim();

  const results = useMemo(() => {
    const list = index.data ?? [];
    if (!query) return list.slice(0, 48);
    const scored: { e: IconEntry; s: number }[] = [];
    for (const e of list) {
      const m = fuzzyMatch(query, e.n.replace(/-/g, " "));
      let s = m?.score ?? -1;
      for (const a of e.a ?? []) {
        const am = fuzzyMatch(query, a);
        if (am && am.score * 0.9 > s) s = am.score * 0.9;
      }
      // Prefer the base icon over -dark/-light variants.
      if (s > 0 && /-(dark|light)$/.test(e.n)) s -= 5;
      if (s > 0) scored.push({ e, s });
    }
    return scored
      .sort((a, b) => b.s - a.s)
      .slice(0, 48)
      .map((x) => x.e);
  }, [index.data, query]);

  const autoName = slugifyIconName(serviceName);
  const tabs: { id: Tab; label: string; icon: typeof Search }[] = [
    { id: "library", label: "Library", icon: Search },
    { id: "site", label: "From site", icon: Globe },
    { id: "upload", label: "Upload", icon: Upload },
    { id: "custom", label: "URL / emoji", icon: Link2 },
  ];

  return (
    <div className="rounded-lg border border-line bg-bg/40">
      <div className="flex items-center gap-1 border-b border-line px-2 pt-2">
        {tabs.map((t) => (
          <button
            type="button"
            key={t.id}
            onClick={() => setTab(t.id)}
            className={clsx(
              "-mb-px flex items-center gap-1.5 rounded-t-md border-b-2 px-2.5 py-1.5 text-xs font-medium transition",
              tab === t.id ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg",
            )}
          >
            <t.icon size={13} />
            {t.label}
          </button>
        ))}
        <button
          type="button"
          onClick={() => onChange("")}
          title={`Automatic (di:${autoName || "…"})`}
          className={clsx(
            "ml-auto mb-1 flex items-center gap-1 rounded-md px-2 py-1 text-xs transition",
            value === "" ? "bg-accent/15 text-accent" : "text-muted hover:text-fg",
          )}
        >
          <Sparkles size={12} /> Auto
        </button>
      </div>

      <div className="p-3">
        {tab === "library" && (
          <>
            <input
              className="input mb-3 py-1.5"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder={serviceName ? `Search icons (showing matches for “${serviceName}”)` : "Search 4,000+ icons…"}
            />
            {index.isLoading && <p className="py-6 text-center text-xs text-muted">Loading icon library…</p>}
            {index.error && <p className="py-6 text-center text-xs text-down">{(index.error as Error).message}</p>}
            <div className="grid max-h-56 grid-cols-[repeat(auto-fill,minmax(64px,1fr))] gap-1.5 overflow-y-auto pr-1">
              {results.map((e) => {
                const ref = `di:${e.n}`;
                return (
                  <button
                    type="button"
                    key={e.n}
                    title={e.n}
                    onClick={() => onChange(ref)}
                    className={clsx(
                      "flex flex-col items-center gap-1 rounded-md p-2 transition",
                      value === ref ? "bg-accent/20 ring-1 ring-accent" : "hover:bg-white/5",
                    )}
                  >
                    <img src={`/icons/di/${e.n}.${e.f}`} alt="" loading="lazy" className="size-8 object-contain" />
                    <span className="w-full truncate text-center text-[10px] text-muted">{e.n}</span>
                  </button>
                );
              })}
            </div>
            {index.data && results.length === 0 && <p className="py-6 text-center text-xs text-muted">No icons match “{query}”.</p>}
          </>
        )}
        {tab === "site" && <FromSite url={serviceUrl} onChange={onChange} />}
        {tab === "upload" && <UploadIcon onChange={onChange} />}
        {tab === "custom" && <CustomIcon value={value} onChange={onChange} />}
      </div>
    </div>
  );
}

function FromSite({ url, onChange }: { url: string; onChange: (v: string) => void }) {
  const [target, setTarget] = useState(url);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const fetchIt = async () => {
    setBusy(true);
    setErr("");
    try {
      const r = await api.post<{ icon: string }>("/api/icons/favicon", { url: target });
      onChange(r.icon);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Failed");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="grid gap-2">
      <p className="text-xs text-muted">LabDash will load the page, find its best icon (including apple-touch-icon and SVG), and store a copy.</p>
      <div className="flex gap-2">
        <input className="input py-1.5 font-mono" value={target} onChange={(e) => setTarget(e.target.value)} placeholder="https://service.home.example.com" />
        <button type="button" className="btn-outline shrink-0 py-1.5" disabled={!target || busy} onClick={fetchIt}>
          {busy ? "Fetching…" : "Fetch icon"}
        </button>
      </div>
      {err && <p className="text-xs text-down">{err}</p>}
    </div>
  );
}

function UploadIcon({ onChange }: { onChange: (v: string) => void }) {
  const ref = useRef<HTMLInputElement>(null);
  const [err, setErr] = useState("");
  const [drag, setDrag] = useState(false);
  const upload = async (file: File) => {
    setErr("");
    const fd = new FormData();
    fd.append("file", file);
    try {
      const r = await api.post<{ icon: string }>("/api/icons/upload", fd);
      onChange(r.icon);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : "Upload failed");
    }
  };
  return (
    <div>
      <button
        type="button"
        onClick={() => ref.current?.click()}
        onDragOver={(e) => {
          e.preventDefault();
          setDrag(true);
        }}
        onDragLeave={() => setDrag(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDrag(false);
          const f = e.dataTransfer.files[0];
          if (f) upload(f);
        }}
        className={clsx(
          "flex h-24 w-full flex-col items-center justify-center gap-1 rounded-md border-2 border-dashed text-xs transition",
          drag ? "border-accent bg-accent/10 text-fg" : "border-line-2 text-muted hover:border-faint",
        )}
      >
        <Upload size={18} />
        Drop an image or click to choose · PNG, SVG, WebP, ICO up to 2 MB
      </button>
      <input ref={ref} type="file" accept="image/*,.ico,.svg" hidden onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
      {err && <p className="mt-2 text-xs text-down">{err}</p>}
    </div>
  );
}

function CustomIcon({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="grid gap-2">
      <input className="input py-1.5 font-mono" value={value} onChange={(e) => onChange(e.target.value)} placeholder="https://…/icon.png · di:jellyfin · emoji:🎬" />
      <div className="flex flex-wrap gap-1">
        {["🎬", "📺", "🎵", "📷", "🏠", "🛡️", "📦", "🧰", "📊", "💾", "🌐", "🤖", "🎮", "📚", "🔒", "⚡"].map((e) => (
          <button type="button" key={e} onClick={() => onChange(`emoji:${e}`)} className="rounded-md p-1.5 text-lg hover:bg-white/10">
            {e}
          </button>
        ))}
      </div>
    </div>
  );
}
