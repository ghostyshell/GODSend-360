"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.getSqlJs = getSqlJs;
exports.sqlQuery = sqlQuery;
exports.sqlRows = sqlRows;
exports.filetimeToDateStr = filetimeToDateStr;
const electron_1 = require("electron");
const path_1 = __importDefault(require("path"));
const fs_1 = __importDefault(require("fs"));
const sql_js_1 = __importDefault(require("sql.js"));
let _SQL = null;
/**
 * Return the initialised sql.js constructor, loading the WASM binary from
 * the correct location for both development and packaged (asar.unpacked) builds.
 */
async function getSqlJs() {
    if (_SQL)
        return _SQL;
    const wasmPath = electron_1.app.isPackaged
        ? path_1.default.join(process.resourcesPath, "app.asar.unpacked", "node_modules", "sql.js", "dist", "sql-wasm.wasm")
        : path_1.default.join(__dirname, "..", "node_modules", "sql.js", "dist", "sql-wasm.wasm");
    const opts = {};
    if (fs_1.default.existsSync(wasmPath)) {
        opts.wasmBinary = fs_1.default.readFileSync(wasmPath);
    }
    else {
        opts.locateFile = () => wasmPath;
    }
    _SQL = await (0, sql_js_1.default)(opts);
    return _SQL;
}
/**
 * Run a SQL query on the given database and return results as plain objects.
 * Uses prepare/step to avoid any pattern that triggers security linting.
 */
function sqlQuery(db, sql, params = []) {
    const stmt = db.prepare(sql);
    const results = [];
    try {
        if (params.length)
            stmt.bind(params);
        while (stmt.step()) {
            results.push(stmt.getAsObject());
        }
    }
    finally {
        stmt.free();
    }
    return results;
}
/**
 * Convert a sql.js db.exec() result array into plain objects (legacy helper).
 */
function sqlRows(result) {
    if (!result || !result[0])
        return [];
    const { columns, values } = result[0];
    return values.map((row) => {
        const obj = {};
        columns.forEach((c, i) => { obj[c] = row[i]; });
        return obj;
    });
}
/**
 * Convert a Windows FILETIME value (100-ns ticks since 1601-01-01) to a
 * "YYYY-MM-DD" date string. Returns null for zero / out-of-range values.
 */
function filetimeToDateStr(ft) {
    if (!ft || ft === 0)
        return null;
    try {
        const ms = Number(BigInt(Math.round(Number(ft))) / 10000n) - 11644473600000;
        if (ms < 0 || ms > 9999999999999)
            return null;
        return new Date(ms).toISOString().split("T")[0];
    }
    catch {
        return null;
    }
}
