import clsx from "clsx";
import type { Status } from "@/lib/types";

const styles: Record<Status, string> = {
  up: "bg-up shadow-[0_0_8px_var(--color-up)]",
  down: "bg-down dot-down",
  pending: "bg-warn",
  maintenance: "bg-maint",
  paused: "bg-faint",
  unknown: "border border-white/30 bg-transparent",
};

const labels: Record<Status, string> = {
  up: "Up",
  down: "Down",
  pending: "Pending",
  maintenance: "Maintenance",
  paused: "Paused",
  unknown: "No monitor",
};

export function StatusDot({ status, className }: { status: Status; className?: string }) {
  return <span title={labels[status]} className={clsx("inline-block size-2.5 shrink-0 rounded-full", styles[status], className)} />;
}

export const statusLabel = (s: Status) => labels[s];
