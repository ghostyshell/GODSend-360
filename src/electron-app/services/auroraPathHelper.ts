import { backendPost } from "../infrastructure/backendHttp";

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
 * Auto-discover the Aurora install path by probing common FTP locations
 * via the Go backend batch endpoint (single FTP connection for all probes).
 * Returns the Aurora root path, or null if not found.
 */
export async function discoverAuroraRoot(xboxIp: string): Promise<string | null> {
  const candidates = [
    ["Hdd1", "Aurora"],
    ["Usb0", "Apps", "Aurora"],
    ["Hdd1", "Apps", "Aurora"],
    ["Usb0", "Aurora"],
    ["Usb1", "Apps", "Aurora"],
    ["Usb1", "Aurora"],
    ["HddX", "Aurora"],
  ];

  // Build one big batch: for each candidate, cd / then cd each segment then
  // cd Data/Databases then pwd.  A failed cd doesn't close the FTP connection,
  // so later candidates still work after a cd / reset.
  const ops: any[] = [];
  const pwdIndices: number[] = [];
  for (const segs of candidates) {
    ops.push({ op: "cd", path: "/" });
    for (const s of segs) ops.push({ op: "cd", path: s });
    ops.push({ op: "cd", path: "Data" });
    ops.push({ op: "cd", path: "Databases" });
    pwdIndices.push(ops.length);
    ops.push({ op: "pwd" });
  }

  const res = await backendPost("/ftp/batch", { ip: xboxIp, ops });
  const results = res.results || [];

  for (let ci = 0; ci < candidates.length; ci++) {
    const r = results[pwdIndices[ci]];
    if (r && r.ok && r.data) {
      const pwd      = String(r.data).replace(/\\/g, "/").replace(/\/+$/, "");
      const expected = "/" + candidates[ci].join("/") + "/Data/Databases";
      if (pwd.toLowerCase() === expected.toLowerCase()) {
        return "/" + candidates[ci].join("/");
      }
    }
  }
  return null;
}

/** Store the last successfully auto-discovered Aurora root. */
export function setLastDiscoveredAuroraRoot(root: string): void {
  _lastDiscoveredAuroraRoot = root;
}
