import clsx from "clsx";
import { useLaunchpad, usePatchSettings } from "@/lib/queries";

/** Global DNS ↔ Direct (IP:port) access-mode switch. */
export function ModeToggle({ variant = "glass" }: { variant?: "glass" | "panel" }) {
  const { data } = useLaunchpad();
  const patch = usePatchSettings();
  const mode = data?.settings.access_mode ?? "dns";
  const set = (m: "dns" | "direct") => m !== mode && patch.mutate({ access_mode: m });

  return (
    <div
      role="radiogroup"
      aria-label="Access mode"
      title="How service links are opened: DNS names or direct IP:port"
      className={clsx("relative flex items-center rounded-full p-0.5 text-xs font-semibold", variant === "glass" ? "glass" : "border border-line-2 bg-panel-2")}
    >
      <span
        aria-hidden
        className={clsx(
          "absolute top-0.5 bottom-0.5 w-[calc(50%-2px)] rounded-full transition-all duration-200",
          mode === "dns" ? "left-0.5 bg-accent" : "left-1/2 bg-accent-2",
        )}
      />
      {(["dns", "direct"] as const).map((m) => (
        <button
          key={m}
          role="radio"
          aria-checked={mode === m}
          onClick={() => set(m)}
          className={clsx("relative z-10 w-14 rounded-full py-1.5 transition-colors", mode === m ? "text-white" : "text-white/50 hover:text-white/80")}
        >
          {m === "dns" ? "DNS" : "IP"}
        </button>
      ))}
    </div>
  );
}
