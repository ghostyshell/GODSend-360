"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.imageExtFromMagic = imageExtFromMagic;
exports.fetchHttpImage = fetchHttpImage;
const http_1 = __importDefault(require("http"));
const https_1 = __importDefault(require("https"));
/**
 * Returns the file-type extension implied by the first magic bytes of a buffer.
 * Defaults to ".jpg" for unrecognised formats.
 */
function imageExtFromMagic(buf) {
    if (!buf || buf.length < 4)
        return ".jpg";
    if (buf[0] === 0xFF && buf[1] === 0xD8)
        return ".jpg";
    if (buf[0] === 0x89 && buf[1] === 0x50 && buf[2] === 0x4E && buf[3] === 0x47)
        return ".png";
    if (buf[0] === 0x47 && buf[1] === 0x49 && buf[2] === 0x46)
        return ".gif";
    return ".jpg";
}
/**
 * Fetch an HTTP/HTTPS URL, follow up to 3 redirects, and return the body as a
 * Buffer. Returns null on error, non-200, or timeout.
 */
function fetchHttpImage(url, redirectCount = 0) {
    return new Promise((resolve) => {
        if (!url || redirectCount > 3) {
            resolve(null);
            return;
        }
        const mod = url.startsWith("https") ? https_1.default : http_1.default;
        const req = mod.get(url, { headers: { "User-Agent": "Aurora/0.7b GODsend" }, timeout: 12000 }, (res) => {
            if (res.statusCode === 301 ||
                res.statusCode === 302 ||
                res.statusCode === 307) {
                const loc = res.headers.location;
                res.resume();
                fetchHttpImage(loc, redirectCount + 1).then(resolve);
                return;
            }
            if (res.statusCode !== 200) {
                res.resume();
                resolve(null);
                return;
            }
            const chunks = [];
            res.on("data", (c) => chunks.push(c));
            res.on("end", () => resolve(Buffer.concat(chunks)));
            res.on("error", () => resolve(null));
        });
        req.on("error", () => resolve(null));
        req.on("timeout", () => { req.destroy(); resolve(null); });
    });
}
