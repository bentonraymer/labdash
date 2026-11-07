import { useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { iconCandidates } from "@/lib/icons";

const palette = ["#5b8cff", "#a78bfa", "#34d27b", "#f5b843", "#ff6b8a", "#22d3ee", "#fb923c"];

/** Renders a service icon, falling through candidate URLs, then a monogram. */
export function ServiceIcon({ icon, name, className }: { icon: string; name: string; className?: string }) {
  const src = useMemo(() => iconCandidates(icon, name), [icon, name]);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => setAttempt(0), [icon, name]);

  if (src.kind === "emoji") {
    return <span className={clsx("flex items-center justify-center leading-none", className)} style={{ fontSize: "2em" }}>{src.value}</span>;
  }
  const url = src.urls[attempt];
  if (!url) {
    const color = palette[[...name].reduce((a, c) => a + c.charCodeAt(0), 0) % palette.length];
    const letters = name
      .split(/[\s\-_]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase())
      .join("");
    return (
      <span
        className={clsx("flex items-center justify-center rounded-xl font-semibold text-white/90", className)}
        style={{ background: `linear-gradient(140deg, ${color}, ${color}88)` }}
      >
        <span className="text-[0.9em]">{letters || "?"}</span>
      </span>
    );
  }
  return (
    <img
      src={url}
      alt=""
      draggable={false}
      loading="lazy"
      className={clsx("object-contain", className)}
      onError={() => setAttempt((a) => a + 1)}
    />
  );
}
