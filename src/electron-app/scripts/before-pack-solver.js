/**
 * electron-builder beforePack hook: make sure the bundled FlareSolverr exists
 * (Linux/Windows x64). Cached after the first download; non-fatal on failure so
 * packaging still succeeds without a solver.
 */
const path = require("path");
const { execFileSync } = require("child_process");

exports.default = async function beforePack() {
  const script = path.resolve(__dirname, "..", "..", "..", "scripts", "download-flaresolverr.js");
  try {
    execFileSync(process.execPath, [script], { stdio: "inherit" });
  } catch (err) {
    console.warn(`[before-pack] FlareSolverr download skipped: ${err.message}`);
  }
};

module.exports = exports.default;
