import { Client } from "basic-ftp";

let _lastDiscoveredAuroraRoot: string | null = null;

/**
 * Derive the Aurora root directory from the configured FTP scripts path.
 *
 * Example mappings:
 *   /Hdd1/Aurora/User/Scripts/Utility/GODSend  →  /Hdd1/Aurora
 *   /Usb0/Apps/Aurora/User/Scripts/Utility/…   →  /Usb0/Apps/Aurora
 */
export function xboxAuroraRoot(ftpScriptsPath: string): string {
  if (ftpScriptsPath) {
    const parts = ftpScriptsPath.replace(/\\/g, "/").split("/").filter(Boolean);
    const idx   = parts.findIndex((p) => p.toLowerCase() === "aurora");
    if (idx !== -1) return "/" + parts.slice(0, idx + 1).join("/");
  }
  if (_lastDiscoveredAuroraRoot) return _lastDiscoveredAuroraRoot;
  return "/Hdd1/Aurora";
}

/** Return the Aurora Media directory (flat cover-art images). */
export function xboxAuroraMediaDir(ftpScriptsPath: string): string {
  return xboxAuroraRoot(ftpScriptsPath) + "/Media";
}

/**
 * Auto-discover the Aurora install path by probing common FTP locations.
 * Returns the Aurora root path, or null if not found.
 */
export async function discoverAuroraRoot(client: Client): Promise<string | null> {
  const candidates = [
    ["Hdd1", "Aurora"],
    ["Usb0", "Apps", "Aurora"],
    ["Hdd1", "Apps", "Aurora"],
    ["Usb0", "Aurora"],
    ["Usb1", "Apps", "Aurora"],
    ["Usb1", "Aurora"],
    ["HddX", "Aurora"],
  ];
  for (const segs of candidates) {
    try {
      await client.cd("/");
      for (const s of segs) await client.cd(s);
      await client.cd("Data");
      await client.cd("Databases");
      const pwd      = (await client.pwd()).replace(/\\/g, "/").replace(/\/+$/, "");
      const expected = "/" + segs.join("/") + "/Data/Databases";
      if (pwd.toLowerCase() === expected.toLowerCase()) {
        return "/" + segs.join("/");
      }
    } catch { /* try next candidate */ }
  }
  return null;
}

/** Store the last successfully auto-discovered Aurora root. */
export function setLastDiscoveredAuroraRoot(root: string): void {
  _lastDiscoveredAuroraRoot = root;
}
