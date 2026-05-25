import { contextBridge, ipcRenderer } from "electron";

contextBridge.exposeInMainWorld("godsendApi", {
  getLogsInfo:              () => ipcRenderer.invoke("logs:get-info"),
  openLogsFolder:           () => ipcRenderer.invoke("logs:open-folder"),
  getStartupEnabled:        () => ipcRenderer.invoke("startup:get"),
  setStartupEnabled:        (enabled: boolean) => ipcRenderer.invoke("startup:set", enabled),
  getOutputBuffer:          () => ipcRenderer.invoke("godsend:get-buffer"),
  startProcess:             () => ipcRenderer.invoke("godsend:start"),
  stopProcess:              () => ipcRenderer.invoke("godsend:stop"),
  restartProcess:           () => ipcRenderer.invoke("godsend:restart"),
  getStoragePath:            () => ipcRenderer.invoke("config:get-storage-path"),
  getEffectiveStoragePath:   () => ipcRenderer.invoke("config:get-effective-storage-path"),
  getDefaultStoragePath:     () => ipcRenderer.invoke("config:get-default-storage-path"),
  setStoragePath:            (folder: string) => ipcRenderer.invoke("config:set-storage-path", folder),
  chooseStoragePath:         () => ipcRenderer.invoke("config:choose-storage-path"),
  getAppDataDir:             () => ipcRenderer.invoke("config:get-app-data-dir"),
  getDefaultAppDataDir:      () => ipcRenderer.invoke("config:get-default-app-data-dir"),
  isPortable:                () => ipcRenderer.invoke("config:is-portable"),
  chooseAppDataDir:          () => ipcRenderer.invoke("config:choose-app-data-dir"),
  setAppDataDir:             (folder: string) => ipcRenderer.invoke("config:set-app-data-dir", folder),
  getTransferFolder:        () => ipcRenderer.invoke("config:get-transfer-folder"),
  getEffectiveTransferFolder: () => ipcRenderer.invoke("config:get-effective-transfer-folder"),
  setTransferFolder:        (folder: string) => ipcRenderer.invoke("config:set-transfer-folder", folder),
  getServerPort:            () => ipcRenderer.invoke("config:get-server-port"),
  setServerPort:            (port: number) => ipcRenderer.invoke("config:set-server-port", port),
  chooseTransferFolder:     () => ipcRenderer.invoke("config:choose-transfer-folder"),
  getArchiveAuth:           () => ipcRenderer.invoke("config:get-archive-auth"),
  loginInternetArchive:     (payload: any) => ipcRenderer.invoke("config:ia-login", payload),
  logoutInternetArchive:    () => ipcRenderer.invoke("config:ia-logout"),
  getROMPath:               () => ipcRenderer.invoke("config:get-rom-path"),
  setROMPath:               (v: string) => ipcRenderer.invoke("config:set-rom-path", v),
  refreshCache:             (platform: string) => ipcRenderer.invoke("config:cache-refresh", platform),
  getXboxConnection:        () => ipcRenderer.invoke("config:get-xbox-connection"),
  setXboxConnection:        (payload: any) => ipcRenderer.invoke("config:set-xbox-connection", payload),
  getFtpScriptsPathDefault: () => ipcRenderer.invoke("config:get-ftp-scripts-path-default"),
  ftpAuroraScripts:         (payload: any) => ipcRenderer.invoke("xbox:ftp-scripts", payload),
  ftpTestConnection:        (payload: any) => ipcRenderer.invoke("xbox:ftp-test", payload),
  ftpScanPorts:             (subnet: string) => ipcRenderer.invoke("xbox:ftp-scan", subnet),

  pingXbox:                 () => ipcRenderer.invoke("xbox:ping"),
  listXboxGames:            () => ipcRenderer.invoke("xbox:list-games"),
  fetchXboxCovers:          (requests: any) => ipcRenderer.invoke("xbox:fetch-covers", requests),
  listAuroraLibrary:        (opts: any) => ipcRenderer.invoke("xbox:list-aurora-library", opts),
  fetchAuroraCovers:        (gameList: any, opts: any) =>
    ipcRenderer.invoke("xbox:fetch-aurora-covers", gameList, opts),
  refreshTitleVisualsFromCache: (payload: any) =>
    ipcRenderer.invoke("xbox:refresh-title-visuals-cache", payload),
  inspectAuroraGame:        (payload: any) => ipcRenderer.invoke("xbox:inspect-aurora-game", payload),
  searchAssets:             (payload: any) => ipcRenderer.invoke("xbox:search-assets", payload),
  fetchUrlImage:            (url: string) => ipcRenderer.invoke("xbox:fetch-url-image", url),
  chooseAssetImageFile:     () => ipcRenderer.invoke("xbox:choose-image-file"),
  uploadAssetToConsole:     (payload: any) => ipcRenderer.invoke("xbox:upload-asset-to-console", payload),
  downloadAllCovers:        (payload: any) => ipcRenderer.invoke("xbox:download-all-covers", payload),
  exportAuroraDb:           () => ipcRenderer.invoke("xbox:export-aurora-db"),
  getQueue:                 () => ipcRenderer.invoke("xbox:get-queue"),
  removeFromQueue:          (game: string) => ipcRenderer.invoke("xbox:remove-queue-item", game),
  getDataStatus:            () => ipcRenderer.invoke("data:status"),
  clearLocalData:           () => ipcRenderer.invoke("data:clear"),
  getAria2ListenPort:       () => ipcRenderer.invoke("config:get-aria2-listen-port"),
  setAria2ListenPort:       (v: string) => ipcRenderer.invoke("config:set-aria2-listen-port", v),
  getAria2DhtPort:          () => ipcRenderer.invoke("config:get-aria2-dht-port"),
  setAria2DhtPort:          (v: string) => ipcRenderer.invoke("config:set-aria2-dht-port", v),
  getDefaultXboxDrive:      () => ipcRenderer.invoke("config:get-default-xbox-drive"),
  setDefaultXboxDrive:      (v: string) => ipcRenderer.invoke("config:set-default-xbox-drive", v),
  getCustomGodPath:         () => ipcRenderer.invoke("config:get-custom-god-path"),
  setCustomGodPath:         (v: string) => ipcRenderer.invoke("config:set-custom-god-path", v),
  getCustomXexPath:         () => ipcRenderer.invoke("config:get-custom-xex-path"),
  setCustomXexPath:         (v: string) => ipcRenderer.invoke("config:set-custom-xex-path", v),
  listXboxDrives:           () => ipcRenderer.invoke("xbox:list-drives"),

  browseGetGames:           (payload: any) => ipcRenderer.invoke("browse:get-games", payload),
  browseFetchCover:         (name: string) => ipcRenderer.invoke("browse:fetch-cover", name),
  browseQueueGame:          (payload: any) => ipcRenderer.invoke("browse:queue-game", payload),
  browseGetDiscInfo:        (game: string) => ipcRenderer.invoke("browse:get-disc-info", game),

  contentDiscover:          (payload: any) => ipcRenderer.invoke("content:discover", payload),
  contentTitleUpdates:      (payload: any) => ipcRenderer.invoke("content:title-updates", payload),
  contentInstalled:         (payload: any) => ipcRenderer.invoke("content:installed", payload),
  contentQueue:             (payload: any) => ipcRenderer.invoke("content:queue", payload),
  contentSources:           (payload: any) => ipcRenderer.invoke("content:sources", payload),
  contentSetActive:         (payload: any) => ipcRenderer.invoke("content:set-active", payload),

  decodeAsset:              (payload: any) => ipcRenderer.invoke("xbox:decode-asset", payload),
  encodeAsset:              (payload: any) => ipcRenderer.invoke("xbox:encode-asset", payload),

  toolsChooseIsoFiles:      () => ipcRenderer.invoke("tools:choose-iso-files"),
  toolsChooseOutputFolder:  () => ipcRenderer.invoke("tools:choose-output-folder"),
  toolsProbeIso:            (isoPath: string) => ipcRenderer.invoke("tools:probe-iso", isoPath),
  toolsIso2God:             (payload: any) => ipcRenderer.invoke("tools:iso2god", payload),
  toolsIso2Xex:             (payload: any) => ipcRenderer.invoke("tools:iso2xex", payload),
  toolsFtpList:             (remotePath: string) => ipcRenderer.invoke("tools:ftp-list", remotePath),
  toolsFtpChooseFiles:      () => ipcRenderer.invoke("tools:ftp-choose-files"),
  toolsFtpChooseFolder:     () => ipcRenderer.invoke("tools:ftp-choose-folder"),
  toolsFtpUpload:           (payload: any) => ipcRenderer.invoke("tools:ftp-upload", payload),
  toolsFtpUploadStatus:     () => ipcRenderer.invoke("tools:ftp-upload-status"),
  toolsFtpUploadRemove:     (id: number) => ipcRenderer.invoke("tools:ftp-upload-remove", id),
  toolsFtpDelete:           (remotePath: string) => ipcRenderer.invoke("tools:ftp-delete", remotePath),
  toolsFtpMkdir:            (remotePath: string) => ipcRenderer.invoke("tools:ftp-mkdir", remotePath),
  toolsFtpRename:           (payload: any) => ipcRenderer.invoke("tools:ftp-rename", payload),
  toolsFtpCopy:             (payload: any) => ipcRenderer.invoke("tools:ftp-copy", payload),
  moveGameToDrive:          (payload: any) => ipcRenderer.invoke("xbox:move-game", payload),

  onFtpDebugLog: (callback: (line: string) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, line: string) => callback(line);
    ipcRenderer.on("godsend-ftp-debug", handler);
    return () => ipcRenderer.removeListener("godsend-ftp-debug", handler);
  },
  onFtpProgress: (callback: (msg: string) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, msg: string) => callback(msg);
    ipcRenderer.on("godsend-ftp-progress", handler);
    return () => ipcRenderer.removeListener("godsend-ftp-progress", handler);
  },
  onXboxCover: (callback: (data: any) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, data: any) => callback(data);
    ipcRenderer.on("xbox-cover", handler);
    return () => ipcRenderer.removeListener("xbox-cover", handler);
  },
  onDownloadCoversProgress: (callback: (data: any) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, data: any) => callback(data);
    ipcRenderer.on("xbox-download-covers-progress", handler);
    return () => ipcRenderer.removeListener("xbox-download-covers-progress", handler);
  },
  onXboxTitleVisuals: (callback: (data: any) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, data: any) => callback(data);
    ipcRenderer.on("xbox-title-visuals", handler);
    return () => ipcRenderer.removeListener("xbox-title-visuals", handler);
  },
  onOutput: (callback: (line: string) => void) => {
    const handler = (_event: Electron.IpcRendererEvent, line: string) => callback(line);
    ipcRenderer.on("godsend-output", handler);
    return () => ipcRenderer.removeListener("godsend-output", handler);
  },
});
