const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("godsendApi", {
  getLogsInfo: () => ipcRenderer.invoke("logs:get-info"),
  openLogsFolder: () => ipcRenderer.invoke("logs:open-folder"),
  getStartupEnabled: () => ipcRenderer.invoke("startup:get"),
  setStartupEnabled: (enabled) => ipcRenderer.invoke("startup:set", enabled),
  getOutputBuffer: () => ipcRenderer.invoke("godsend:get-buffer"),
  startProcess: () => ipcRenderer.invoke("godsend:start"),
  stopProcess: () => ipcRenderer.invoke("godsend:stop"),
  restartProcess: () => ipcRenderer.invoke("godsend:restart"),
  getTransferFolder: () => ipcRenderer.invoke("config:get-transfer-folder"),
  getEffectiveTransferFolder: () => ipcRenderer.invoke("config:get-effective-transfer-folder"),
  setTransferFolder: (folder) => ipcRenderer.invoke("config:set-transfer-folder", folder),
  getServerPort: () => ipcRenderer.invoke("config:get-server-port"),
  setServerPort: (port) => ipcRenderer.invoke("config:set-server-port", port),
  chooseTransferFolder: () => ipcRenderer.invoke("config:choose-transfer-folder"),
  getArchiveAuth: () => ipcRenderer.invoke("config:get-archive-auth"),
  loginInternetArchive: (payload) => ipcRenderer.invoke("config:ia-login", payload),
  logoutInternetArchive: () => ipcRenderer.invoke("config:ia-logout"),
  getIAConcurrency: () => ipcRenderer.invoke("config:get-ia-concurrency"),
  setIAConcurrency: (v) => ipcRenderer.invoke("config:set-ia-concurrency", v),
  getROMPath: () => ipcRenderer.invoke("config:get-rom-path"),
  setROMPath: (v) => ipcRenderer.invoke("config:set-rom-path", v),
  refreshCache: (platform) => ipcRenderer.invoke("config:cache-refresh", platform),
  getXboxConnection: () => ipcRenderer.invoke("config:get-xbox-connection"),
  setXboxConnection: (payload) => ipcRenderer.invoke("config:set-xbox-connection", payload),
  getFtpScriptsPathDefault: () => ipcRenderer.invoke("config:get-ftp-scripts-path-default"),
  ftpAuroraScripts: (payload) => ipcRenderer.invoke("xbox:ftp-scripts", payload),
  ftpTestConnection: (payload) => ipcRenderer.invoke("xbox:ftp-test", payload),
  ftpScanPorts: (subnet) => ipcRenderer.invoke("xbox:ftp-scan", subnet),

  pingXbox: () => ipcRenderer.invoke("xbox:ping"),
  listXboxGames: () => ipcRenderer.invoke("xbox:list-games"),
  fetchXboxCovers: (requests) => ipcRenderer.invoke("xbox:fetch-covers", requests),

  // Each subscription function returns a cleanup function for React useEffect.
  onFtpDebugLog: (callback) => {
    const handler = (_event, line) => callback(line);
    ipcRenderer.on("godsend-ftp-debug", handler);
    return () => ipcRenderer.removeListener("godsend-ftp-debug", handler);
  },
  onFtpProgress: (callback) => {
    const handler = (_event, msg) => callback(msg);
    ipcRenderer.on("godsend-ftp-progress", handler);
    return () => ipcRenderer.removeListener("godsend-ftp-progress", handler);
  },
  onXboxCover: (callback) => {
    const handler = (_event, data) => callback(data);
    ipcRenderer.on("xbox-cover", handler);
    return () => ipcRenderer.removeListener("xbox-cover", handler);
  },
  onOutput: (callback) => {
    const handler = (_event, line) => callback(line);
    ipcRenderer.on("godsend-output", handler);
    return () => ipcRenderer.removeListener("godsend-output", handler);
  },
});
