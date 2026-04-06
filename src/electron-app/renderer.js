const outputEl           = document.getElementById("output");
const startupCheckbox    = document.getElementById("startupCheckbox");
const restartBtn         = document.getElementById("restartBtn");
const settingsBtn        = document.getElementById("settingsBtn");
const backBtn            = document.getElementById("backBtn");
const transferPathEl     = document.getElementById("transferPath");
const transferBrowseBtn  = document.getElementById("transferBrowseBtn");
const transferResetBtn   = document.getElementById("transferResetBtn");
const iaSessionStatusEl  = document.getElementById("iaSessionStatus");
const iaEmailEl          = document.getElementById("iaEmail");
const iaPasswordEl       = document.getElementById("iaPassword");
const iaLoginBtn         = document.getElementById("iaLoginBtn");
const iaLogoutBtn        = document.getElementById("iaLogoutBtn");
const iaConcurrencyEl    = document.getElementById("iaConcurrency");
const iaConcurrencyValEl = document.getElementById("iaConcurrencyVal");
const romPathEl          = document.getElementById("romPath");
const romPathSaveBtn     = document.getElementById("romPathSaveBtn");
const romPathResetBtn    = document.getElementById("romPathResetBtn");
const cacheRefreshBtn    = document.getElementById("cacheRefreshBtn");
const cacheRefreshStatus = document.getElementById("cacheRefreshStatus");
const xboxIpEl               = document.getElementById("xboxIp");
const ftpUserEl              = document.getElementById("ftpUser");
const ftpPasswordEl          = document.getElementById("ftpPassword");
const ftpScriptsPathEl       = document.getElementById("ftpScriptsPath");
const ftpScriptsPathResetBtn = document.getElementById("ftpScriptsPathResetBtn");
const ftpScriptsBtn          = document.getElementById("ftpScriptsBtn");
const ftpScriptsStatus       = document.getElementById("ftpScriptsStatus");
const pageHome           = document.getElementById("page-home");
const pageSettings       = document.getElementById("page-settings");
const logPathHint        = document.getElementById("logPathHint");
const openLogsBtn        = document.getElementById("openLogsBtn");

// ── Page navigation ──

function showPage(page) {
  pageHome.classList.remove("active");
  pageSettings.classList.remove("active");
  page.classList.add("active");
}

// ── Helpers ──

function appendLine(line) {
  outputEl.textContent += `${line}\n`;
  outputEl.scrollTop = outputEl.scrollHeight;
}

function updateIASessionUI(auth) {
  const has = auth && auth.hasSession;
  iaSessionStatusEl.textContent =
    has && auth.iaScreenname ? `Signed in as ${auth.iaScreenname}.`
    : has                    ? `Signed in (${auth.iaEmail || "session active"}).`
    :                          "Not signed in.";
}

async function refreshTransferPathField() {
  transferPathEl.value = (await window.godsendApi.getEffectiveTransferFolder()) || "";
}

// ── Init ──

async function initialize() {
  startupCheckbox.checked = await window.godsendApi.getStartupEnabled();

  await refreshTransferPathField();

  const auth = await window.godsendApi.getArchiveAuth();
  iaEmailEl.value = auth.iaEmail || "";
  iaPasswordEl.value = "";
  updateIASessionUI(auth);

  const concurrency = await window.godsendApi.getIAConcurrency();
  iaConcurrencyEl.value = concurrency;
  iaConcurrencyValEl.textContent = String(concurrency);

  romPathEl.value = await window.godsendApi.getROMPath();

  const xboxConn = await window.godsendApi.getXboxConnection();
  xboxIpEl.value         = xboxConn.xboxIp         || "";
  ftpUserEl.value        = xboxConn.ftpUser         || "";
  ftpPasswordEl.value    = xboxConn.ftpPassword     || "";
  ftpScriptsPathEl.value = xboxConn.ftpScriptsPath  || "";

  const lines = await window.godsendApi.getOutputBuffer();
  outputEl.textContent = lines.join("\n");
  outputEl.scrollTop = outputEl.scrollHeight;

  const logInfo = await window.godsendApi.getLogsInfo();
  if (logInfo && logPathHint) {
    logPathHint.textContent = `Log: ${logInfo.currentLogFile || ""}`;
    logPathHint.title = logInfo.logsDirectory || "";
  }
}

// ── Home page ──

restartBtn.addEventListener("click", async () => {
  await window.godsendApi.restartProcess();
});

settingsBtn.addEventListener("click", () => showPage(pageSettings));

if (openLogsBtn) {
  openLogsBtn.addEventListener("click", async () => {
    const r = await window.godsendApi.openLogsFolder();
    if (r && !r.ok && r.error) {
      appendLine(`[ERROR] Could not open logs folder: ${r.error}`);
    }
  });
}

// ── Settings page ──

backBtn.addEventListener("click", () => showPage(pageHome));

startupCheckbox.addEventListener("change", async () => {
  startupCheckbox.checked = await window.godsendApi.setStartupEnabled(startupCheckbox.checked);
});

transferBrowseBtn.addEventListener("click", async () => {
  const picked = await window.godsendApi.chooseTransferFolder();
  if (!picked) return;
  await window.godsendApi.setTransferFolder(picked);
  await refreshTransferPathField();
});

transferResetBtn.addEventListener("click", async () => {
  await window.godsendApi.setTransferFolder("");
  await refreshTransferPathField();
});

iaLoginBtn.addEventListener("click", async () => {
  iaLoginBtn.disabled = true;
  try {
    const r = await window.godsendApi.loginInternetArchive({
      email: iaEmailEl.value,
      password: iaPasswordEl.value
    });
    iaPasswordEl.value = "";
    if (r.ok) {
      appendLine("[INFO] Internet Archive: signed in; backend restarted.");
      updateIASessionUI(await window.godsendApi.getArchiveAuth());
    } else {
      appendLine(`[ERROR] Internet Archive login: ${r.error || "Unknown error"}`);
    }
  } finally {
    iaLoginBtn.disabled = false;
  }
});

iaLogoutBtn.addEventListener("click", async () => {
  await window.godsendApi.logoutInternetArchive();
  updateIASessionUI(await window.godsendApi.getArchiveAuth());
  appendLine("[INFO] Internet Archive: signed out; backend restarted.");
});

iaConcurrencyEl.addEventListener("input", () => {
  iaConcurrencyValEl.textContent = iaConcurrencyEl.value;
});

iaConcurrencyEl.addEventListener("change", async () => {
  await window.godsendApi.setIAConcurrency(parseInt(iaConcurrencyEl.value));
});

romPathSaveBtn.addEventListener("click", async () => {
  await window.godsendApi.setROMPath(romPathEl.value);
});

cacheRefreshBtn.addEventListener("click", async () => {
  cacheRefreshBtn.disabled = true;
  cacheRefreshStatus.textContent = "Requesting refresh...";
  const r = await window.godsendApi.refreshCache("all");
  cacheRefreshStatus.textContent = r.ok
    ? "Refresh started — running in background. Check server log for progress."
    : `Failed: ${r.error || "unknown error"}`;
  cacheRefreshBtn.disabled = false;
});

romPathResetBtn.addEventListener("click", async () => {
  await window.godsendApi.setROMPath("");
  romPathEl.value = await window.godsendApi.getROMPath();
});

ftpScriptsPathResetBtn.addEventListener("click", async () => {
  ftpScriptsPathEl.value = await window.godsendApi.getFtpScriptsPathDefault();
});

ftpScriptsBtn.addEventListener("click", async () => {
  const xboxIp = xboxIpEl.value.trim();
  if (!xboxIp) {
    ftpScriptsStatus.textContent = "Enter the Xbox IP address first.";
    return;
  }
  ftpScriptsBtn.disabled = true;
  ftpScriptsStatus.textContent = "Starting…";
  try {
    // Save all connection settings before uploading so they persist across sessions.
    await window.godsendApi.setXboxConnection({
      xboxIp,
      ftpUser:        ftpUserEl.value.trim(),
      ftpPassword:    ftpPasswordEl.value,
      ftpScriptsPath: ftpScriptsPathEl.value.trim(),
    });
    const r = await window.godsendApi.ftpAuroraScripts({
      xboxIp,
      ftpUser:        ftpUserEl.value.trim(),
      ftpPassword:    ftpPasswordEl.value,
      ftpScriptsPath: ftpScriptsPathEl.value.trim(),
    });
    ftpScriptsStatus.textContent = r.ok
      ? `Aurora scripts uploaded successfully to ${r.remotePath || "(path unknown)"}.`
      : `Failed: ${r.error || "unknown error"}`;
  } catch (err) {
    ftpScriptsStatus.textContent = `Failed: ${err.message || "unknown error"}`;
  } finally {
    ftpScriptsBtn.disabled = false;
  }
});

// ── Live output ──

window.godsendApi.onOutput((line) => appendLine(line));

// ── FTP progress ──

window.godsendApi.onFtpProgress((msg) => {
  ftpScriptsStatus.textContent = msg;
});

initialize().catch((err) => appendLine(`[ERROR] UI init failed: ${err.message}`));
