import { describe, expect, it } from "vitest";
import { fuzzyMatch } from "./fuzzy";

const rank = (q: string, items: string[]) =>
  items
    .map((t) => ({ t, m: fuzzyMatch(q, t) }))
    .filter((x) => x.m)
    .sort((a, b) => b.m!.score - a.m!.score)
    .map((x) => x.t);

describe("fuzzyMatch", () => {
  const items = ["Jellyfin", "Jellyseerr", "HomeAssistant", "Home Assistant", "Paperless-ngx", "Pi-hole", "Proxmox Datacenter Manager", "Prowlarr"];

  it("prefers prefix matches", () => {
    expect(rank("jelly", items).slice(0, 2)).toEqual(["Jellyfin", "Jellyseerr"]);
  });
  it("matches word starts", () => {
    expect(rank("assist", items).slice(0, 2).sort()).toEqual(["Home Assistant", "HomeAssistant"]);
  });
  it("matches acronyms", () => {
    expect(rank("pdm", items)[0]).toBe("Proxmox Datacenter Manager");
  });
  it("matches camelCase word starts", () => {
    expect(fuzzyMatch("assist", "HomeAssistant")).not.toBeNull();
  });
  it("matches subsequences and rejects non-matches", () => {
    expect(fuzzyMatch("pprls", "Paperless-ngx")).not.toBeNull();
    expect(fuzzyMatch("zzz", "Jellyfin")).toBeNull();
  });
  it("exact beats prefix", () => {
    expect(rank("prowlarr", ["Prowlarr Backup", "Prowlarr"])[0]).toBe("Prowlarr");
  });
});
