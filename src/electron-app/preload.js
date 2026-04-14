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
  listAuroraLibrary: (opts) => ipcRenderer.invoke("xbox:list-aurora-library", opts),
  fetchAuroraCovers: (gameList, opts) =>
    ipcRenderer.invoke("xbox:fetch-aurora-covers", gameList, opts),
  refreshTitleVisualsFromCache: (payload) =>
    ipcRenderer.invoke("xbox:refresh-title-visuals-cache", payload),
  inspectAuroraGame: (payload) => ipcRenderer.invoke("xbox:inspect-aurora-game", payload),
  searchAssets: (payload) => ipcRenderer.invoke("xbox:search-assets", payload),
  fetchUrlImage: (url) => ipcRenderer.invoke("xbox:fetch-url-image", url),
  chooseAssetImageFile: () => ipcRenderer.invoke("xbox:choose-image-file"),
  uploadAssetToConsole: (payload) => ipcRenderer.invoke("xbox:upload-asset-to-console", payload),
  getAuroraLibrarySources: () => ipcRenderer.invoke("config:get-aurora-library-sources"),
  setAuroraLibrarySources: (sources) => ipcRenderer.invoke("config:set-aurora-library-sources", sources),
  getQueue: () => ipcRenderer.invoke("xbox:get-queue"),
  removeFromQueue: (game) => ipcRenderer.invoke("xbox:remove-queue-item", game),
  getDataStatus: () => ipcRenderer.invoke("data:status"),
  clearLocalData: () => ipcRenderer.invoke("data:clear"),
  getAria2ListenPort: () => ipcRenderer.invoke("config:get-aria2-listen-port"),
  setAria2ListenPort: (v) => ipcRenderer.invoke("config:set-aria2-listen-port", v),
  getAria2DhtPort: () => ipcRenderer.invoke("config:get-aria2-dht-port"),
  setAria2DhtPort: (v) => ipcRenderer.invoke("config:set-aria2-dht-port", v),
  getDefaultXboxDrive: () => ipcRenderer.invoke("config:get-default-xbox-drive"),
  setDefaultXboxDrive: (v) => ipcRenderer.invoke("config:set-default-xbox-drive", v),
  listXboxDrives: () => ipcRenderer.invoke("xbox:list-drives"),

  browseGetGames:    (payload) => ipcRenderer.invoke("browse:get-games", payload),
  browseFetchCover:  (name) => ipcRenderer.invoke("browse:fetch-cover", name),
  browseQueueGame:   (payload) => ipcRenderer.invoke("browse:queue-game", payload),
  browseGetDiscInfo: (game)    => ipcRenderer.invoke("browse:get-disc-info", game),

  decodeAsset: (payload) => ipcRenderer.invoke("xbox:decode-asset", payload),
  encodeAsset: (payload) => ipcRenderer.invoke("xbox:encode-asset", payload),

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
  onXboxTitleVisuals: (callback) => {
    const handler = (_event, data) => callback(data);
    ipcRenderer.on("xbox-title-visuals", handler);
    return () => ipcRenderer.removeListener("xbox-title-visuals", handler);
  },
  onOutput: (callback) => {
    const handler = (_event, line) => callback(line);
    ipcRenderer.on("godsend-output", handler);
    return () => ipcRenderer.removeListener("godsend-output", handler);
  },
});
