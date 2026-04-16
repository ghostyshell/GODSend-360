"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.backendGet = backendGet;
exports.backendPost = backendPost;
const http_1 = __importDefault(require("http"));
const settingsService_1 = require("../services/settingsService");
/**
 * Fire a GET request to the local Go backend and resolve with the raw response
 * body text. Rejects on network error or 30-second timeout.
 */
function backendGet(urlPath) {
    const port = (0, settingsService_1.getConfiguredServerPort)();
    return new Promise((resolve, reject) => {
        const req = http_1.default.get(`http://localhost:${port}${urlPath}`, (res) => {
            let data = "";
            res.on("data", (c) => { data += c; });
            res.on("end", () => resolve(data));
        });
        req.on("error", reject);
        req.setTimeout(30000, () => { req.destroy(); reject(new Error("Timeout")); });
    });
}
/**
 * Fire a POST request with a JSON body to the local Go backend and resolve
 * with the parsed JSON response. Rejects on network error or timeout.
 */
function backendPost(urlPath, body, timeoutMs = 600000) {
    const port = (0, settingsService_1.getConfiguredServerPort)();
    const payload = JSON.stringify(body);
    return new Promise((resolve, reject) => {
        const opts = {
            hostname: "localhost",
            port,
            path: urlPath,
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                "Content-Length": Buffer.byteLength(payload),
            },
        };
        const req = http_1.default.request(opts, (res) => {
            let data = "";
            res.on("data", (c) => { data += c; });
            res.on("end", () => {
                try {
                    resolve(JSON.parse(data));
                }
                catch {
                    resolve({ error: data });
                }
            });
        });
        req.on("error", reject);
        req.setTimeout(timeoutMs, () => { req.destroy(); reject(new Error("Timeout")); });
        req.write(payload);
        req.end();
    });
}
