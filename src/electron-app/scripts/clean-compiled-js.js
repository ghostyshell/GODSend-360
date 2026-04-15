const fs = require("fs");
const path = require("path");

const projectRoot = path.resolve(__dirname, "..");

const tsRoots = [
  "main.ts",
  "preload.ts",
  "app",
  "services",
  "infrastructure",
  "ipc",
];

function walk(dirPath, outFiles) {
  const entries = fs.readdirSync(dirPath, { withFileTypes: true });
  for (const entry of entries) {
    const full = path.join(dirPath, entry.name);
    if (entry.isDirectory()) {
      walk(full, outFiles);
      continue;
    }
    if (entry.isFile() && full.endsWith(".ts") && !full.endsWith(".d.ts")) {
      outFiles.push(full);
    }
  }
}

function collectTsFiles() {
  const tsFiles = [];
  for (const rel of tsRoots) {
    const full = path.join(projectRoot, rel);
    if (!fs.existsSync(full)) continue;
    const stat = fs.statSync(full);
    if (stat.isDirectory()) {
      walk(full, tsFiles);
    } else if (stat.isFile() && full.endsWith(".ts")) {
      tsFiles.push(full);
    }
  }
  return tsFiles;
}

function cleanupCompiledJs() {
  const tsFiles = collectTsFiles();
  let removed = 0;

  for (const tsFile of tsFiles) {
    const jsFile = tsFile.slice(0, -3) + ".js";
    if (fs.existsSync(jsFile)) {
      fs.unlinkSync(jsFile);
      removed += 1;
    }
  }

  console.log(`clean-compiled-js: removed ${removed} compiled file(s).`);
}

cleanupCompiledJs();
