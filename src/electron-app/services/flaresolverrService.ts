/**
 * Optional bundled FlareSolverr.
 *
 * If vendor/flaresolverr/ contains a binary (fetched by
 * scripts/download-flaresolverr.js and shipped through extraResources), start
 * it on a free local port and expose its URL so the Go backend can clear
 * Cloudflare challenges on dlxbgame ad gates and container pages.
 *
 * No binary (macOS, or the download was skipped) means this is a no-op; a
 * user-configured solver URL in Settings still works.
 */
import { spawn, ChildProcess } from "child_process";
import fs from "fs";
import http from "http";
import net from "net";
import path from "path";
import { getBundledResourcesRoot, getRepoRoot } from "../infrastructure/fileSystem";

let child: ChildProcess | null = null;
let childUrl = "";

function solverBinary(): string | null {
  const exe = process.platform === "win32" ? "flaresolverr.exe" : "flaresolverr";
  const roots = [
    path.join(getBundledResourcesRoot(), "flaresolverr"),
    path.join(getRepoRoot(), "vendor", "flaresolverr"),
  ];
  for (const root of roots) {
    const direct = path.join(root, exe);
    if (fs.existsSync(direct)) return direct;
    // Some archives keep a nested folder.
    try {
      for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
        if (!entry.isDirectory()) continue;
        const nested = path.join(root, entry.name, exe);
        if (fs.existsSync(nested)) return nested;
      }
    } catch {
      // root missing - try the next candidate
    }
  }
  return null;
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      srv.close(() => resolve(port));
    });
  });
}

function waitHealthy(url: string, timeoutMs: number): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve) => {
    let settled = false;
    const finish = (ok: boolean) => {
      if (settled) return;
      settled = true;
      resolve(ok);
    };
    const tick = () => {
      if (Date.now() > deadline) return finish(false);
      let handled = false;
      const retry = () => {
        if (handled) return;
        handled = true;
        setTimeout(tick, 500);
      };
      const req = http.get(`${url}/health`, (res) => {
        res.resume();
        if (res.statusCode === 200) {
          handled = true;
          finish(true);
          return;
        }
        retry();
      });
      req.on("error", retry);
      // destroy() without an error emits "close", not "error" - schedule the
      // next probe here too so a hung /health can never stall startup.
      req.setTimeout(2000, () => {
        req.destroy();
        retry();
      });
    };
    tick();
  });
}

/** True when a solver answers /health at url within timeoutMs. */
function healthy(url: string, timeoutMs = 1500): Promise<boolean> {
  return new Promise((resolve) => {
    const req = http.get(`${url}/health`, (res) => {
      res.resume();
      resolve(res.statusCode === 200);
    });
    req.on("error", () => resolve(false));
    req.setTimeout(timeoutMs, () => {
      req.destroy();
      resolve(false);
    });
  });
}

/** Start the bundled solver if present; resolves with its base URL or "". */
export async function ensureLocalSolver(appendLine?: (line: string) => void): Promise<string> {
  if (childUrl) return childUrl;

  // Never run two solvers side by side: reuse one that is already listening
  // (the app's own previous run, Byparr on 8191, or a manually started one).
  for (const existing of ["http://127.0.0.1:8191", "http://127.0.0.1:8192"]) {
    if (await healthy(existing)) {
      childUrl = existing;
      appendLine?.(`[INFO] FlareSolverr: reusing existing solver at ${existing}`);
      return existing;
    }
  }

  const bin = solverBinary();
  if (!bin) return "";
  try {
    const port = await freePort();
    // Minimal, scrubbed environment: the solver is third-party and has no
    // need for the app's secrets. HOST keeps its unauthenticated listener on
    // loopback instead of the whole LAN.
    const childEnv: NodeJS.ProcessEnv = {
      PATH: process.env.PATH,
      HOME: process.env.HOME,
      USERPROFILE: process.env.USERPROFILE,
      TMPDIR: process.env.TMPDIR,
      TEMP: process.env.TEMP,
      SystemRoot: process.env.SystemRoot,
      WINDIR: process.env.WINDIR,
      PORT: String(port),
      HOST: "127.0.0.1",
      LOG_LEVEL: "warning",
    };
    child = spawn(bin, ["--port", String(port)], {
      cwd: path.dirname(bin),
      env: childEnv,
      stdio: "ignore",
      windowsHide: true,
    });
    child.on("exit", () => {
      child = null;
      childUrl = "";
    });
    const url = `http://127.0.0.1:${port}`;
    if (await waitHealthy(url, 20000)) {
      childUrl = url;
      appendLine?.(`[INFO] FlareSolverr: bundled solver running at ${url}`);
      return url;
    }
    appendLine?.(`[WARN] FlareSolverr: bundled solver did not become healthy; continuing without it`);
    child?.kill();
    child = null;
    return "";
  } catch (err: any) {
    appendLine?.(`[WARN] FlareSolverr: could not start bundled solver (${err?.message || err})`);
    child = null;
    return "";
  }
}

/** Stop the bundled solver on app quit. */
export function stopLocalSolver(): void {
  if (child) {
    try {
      child.kill();
    } catch {
      // already gone
    }
    child = null;
    childUrl = "";
  }
}
