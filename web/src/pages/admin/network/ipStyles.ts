import type { CellState } from "@/lib/types";

/** Cell styling per IP state. The last octet is always printed, and the
 *  legend + tooltip name the state, so color is never the only cue. */
export const cellStyle: Record<CellState, string> = {
  network: "bg-transparent text-faint/60 border border-dashed border-line",
  broadcast: "bg-transparent text-faint/60 border border-dashed border-line",
  gateway: "bg-white/85 text-bg font-semibold",
  static: "bg-accent text-white",
  service: "bg-accent-2 text-white",
  reserved: "bg-[repeating-linear-gradient(135deg,rgba(255,255,255,0.14)_0_3px,transparent_3px_7px)] border border-white/25 text-fg",
  dhcp: "bg-teal-500/20 text-teal-200",
  discovered: "bg-warn text-bg font-semibold",
  free: "bg-white/[0.04] text-faint hover:bg-white/10",
  unmanaged: "bg-white/[0.04] text-faint",
};

export const stateLabel: Record<CellState, string> = {
  network: "Network",
  broadcast: "Broadcast",
  gateway: "Gateway",
  static: "Static / host",
  service: "Service",
  reserved: "Reserved",
  dhcp: "DHCP",
  discovered: "Seen, unclaimed",
  free: "Free",
  unmanaged: "Not in a subnet",
};

export const legendOrder: CellState[] = ["gateway", "static", "service", "reserved", "dhcp", "discovered", "free"];
