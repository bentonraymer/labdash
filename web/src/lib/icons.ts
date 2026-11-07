// Icon references stored on a service/section:
//   ""              → auto: guess a dashboard-icons name from the service name
//   "di:jellyfin"   → dashboard-icons (cached by the backend under /icons/di)
//   "https://…" or "/…" → an explicit image URL
//   "emoji:🚀"      → an emoji

export function slugifyIconName(name: string): string {
  return name
    .trim()
    .replace(/([a-z])([A-Z])/g, "$1-$2")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export type IconSource = { kind: "urls"; urls: string[] } | { kind: "emoji"; value: string };

export function iconCandidates(icon: string, fallbackName: string): IconSource {
  if (icon.startsWith("emoji:")) return { kind: "emoji", value: icon.slice(6) };
  if (icon.startsWith("http://") || icon.startsWith("https://") || icon.startsWith("/")) {
    return { kind: "urls", urls: [icon] };
  }
  const name = icon.startsWith("di:") ? icon.slice(3) : slugifyIconName(fallbackName);
  if (!name) return { kind: "urls", urls: [] };
  const names = [name];
  // "home-assistant" also commonly exists as "homeassistant" and vice versa.
  if (name.includes("-")) names.push(name.replace(/-/g, ""));
  return { kind: "urls", urls: names.flatMap((n) => [`/icons/di/${n}.svg`, `/icons/di/${n}.png`]) };
}
