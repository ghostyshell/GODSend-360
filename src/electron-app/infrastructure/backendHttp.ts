import http from "http";
import { getConfiguredServerPort } from "../services/settingsService";

/**
 * Fire a GET request to the local Go backend and resolve with the raw response
 * body text. Rejects on network error or 30-second timeout.
 */
export function backendGet(urlPath: string): Promise<string> {
  const port = getConfiguredServerPort();
  return new Promise((resolve, reject) => {
    const req = http.get(`http://localhost:${port}${urlPath}`, (res) => {
      let data = "";
      res.on("data", (c: Buffer) => { data += c; });
      res.on("end",  () => resolve(data));
    });
    req.on("error", reject);
    req.setTimeout(30000, () => { req.destroy(); reject(new Error("Timeout")); });
  });
}

/**
 * Fire a POST request with a JSON body to the local Go backend and resolve
 * with the parsed JSON response. Rejects on network error or timeout.
 */
export function backendPost(urlPath: string, body: object, timeoutMs = 600000): Promise<any> {
  const port    = getConfiguredServerPort();
  const payload = JSON.stringify(body);
  return new Promise((resolve, reject) => {
    const opts: http.RequestOptions = {
      hostname: "localhost",
      port,
      path:     urlPath,
      method:   "POST",
      headers: {
        "Content-Type":   "application/json",
        "Content-Length": Buffer.byteLength(payload),
      },
    };
    const req = http.request(opts, (res) => {
      let data = "";
      res.on("data", (c: Buffer) => { data += c; });
      res.on("end",  () => {
        try { resolve(JSON.parse(data)); }
        catch { resolve({ error: data }); }
      });
    });
    req.on("error", reject);
    req.setTimeout(timeoutMs, () => { req.destroy(); reject(new Error("Timeout")); });
    req.write(payload);
    req.end();
  });
}
