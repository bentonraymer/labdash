export type AccessMode = "dns" | "direct";
export type Status = "up" | "down" | "pending" | "maintenance" | "paused" | "unknown";
export type TileSize = "sm" | "md" | "lg";

export interface User {
  id: number;
  username: string;
}

export interface Settings {
  site_title: string;
  access_mode: AccessMode;
  dns_domain: string;
  wallpaper: string;
  wallpaper_blur: number;
  wallpaper_dim: number;
  alerts_paused: boolean;
  tile_glow: boolean;
  open_new_tab: boolean;
}

export interface Section {
  id: number;
  name: string;
  icon: string;
  position: number;
  collapsed: boolean;
  tileSize: TileSize;
}

export interface QuickLink {
  label: string;
  url: string;
}

export interface Service {
  id: number;
  sectionId: number | null;
  name: string;
  icon: string;
  description: string;
  position: number;
  dnsUrl: string;
  directScheme: "http" | "https";
  directIp: string;
  directPort: number | null;
  directPath: string;
  quickLinks: QuickLink[];
  tags: string[];
  newTab: boolean;
  url: string;
  directUrl: string;
  status: Status;
}

export interface Launchpad {
  sections: Section[];
  services: Service[];
  settings: Settings;
}

export interface SpotlightUsage {
  kind: string;
  ref: string;
  count: number;
  lastUsed: string;
}
