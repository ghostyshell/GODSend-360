"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const electron_1 = require("electron");
electron_1.contextBridge.exposeInMainWorld("godsendApi", {
    getLogsInfo: () => electron_1.ipcRenderer.invoke("logs:get-info"),
    openLogsFolder: () => electron_1.ipcRenderer.invoke("logs:open-folder"),
    getStartupEnabled: () => electron_1.ipcRenderer.invoke("startup:get"),
    setStartupEnabled: (enabled) => electron_1.ipcRenderer.invoke("startup:set", enabled),
    getOutputBuffer: () => electron_1.ipcRenderer.invoke("godsend:get-buffer"),
    startProcess: () => electron_1.ipcRenderer.invoke("godsend:start"),
    stopProcess: () => electron_1.ipcRenderer.invoke("godsend:stop"),
    restartProcess: () => electron_1.ipcRenderer.invoke("godsend:restart"),
    getTransferFolder: () => electron_1.ipcRenderer.invoke("config:get-transfer-folder"),
    getEffectiveTransferFolder: () => electron_1.ipcRenderer.invoke("config:get-effective-transfer-folder"),
    setTransferFolder: (folder) => electron_1.ipcRenderer.invoke("config:set-transfer-folder", folder),
    getServerPort: () => electron_1.ipcRenderer.invoke("config:get-server-port"),
    setServerPort: (port) => electron_1.ipcRenderer.invoke("config:set-server-port", port),
    chooseTransferFolder: () => electron_1.ipcRenderer.invoke("config:choose-transfer-folder"),
    getArchiveAuth: () => electron_1.ipcRenderer.invoke("config:get-archive-auth"),
    loginInternetArchive: (payload) => electron_1.ipcRenderer.invoke("config:ia-login", payload),
    logoutInternetArchive: () => electron_1.ipcRenderer.invoke("config:ia-logout"),
    getROMPath: () => electron_1.ipcRenderer.invoke("config:get-rom-path"),
    setROMPath: (v) => electron_1.ipcRenderer.invoke("config:set-rom-path", v),
    refreshCache: (platform) => electron_1.ipcRenderer.invoke("config:cache-refresh", platform),
    getXboxConnection: () => electron_1.ipcRenderer.invoke("config:get-xbox-connection"),
    setXboxConnection: (payload) => electron_1.ipcRenderer.invoke("config:set-xbox-connection", payload),
    getFtpScriptsPathDefault: () => electron_1.ipcRenderer.invoke("config:get-ftp-scripts-path-default"),
    ftpAuroraScripts: (payload) => electron_1.ipcRenderer.invoke("xbox:ftp-scripts", payload),
    ftpTestConnection: (payload) => electron_1.ipcRenderer.invoke("xbox:ftp-test", payload),
    ftpScanPorts: (subnet) => electron_1.ipcRenderer.invoke("xbox:ftp-scan", subnet),
    pingXbox: () => electron_1.ipcRenderer.invoke("xbox:ping"),
    listXboxGames: () => electron_1.ipcRenderer.invoke("xbox:list-games"),
    fetchXboxCovers: (requests) => electron_1.ipcRenderer.invoke("xbox:fetch-covers", requests),
    listAuroraLibrary: (opts) => electron_1.ipcRenderer.invoke("xbox:list-aurora-library", opts),
    fetchAuroraCovers: (gameList, opts) => electron_1.ipcRenderer.invoke("xbox:fetch-aurora-covers", gameList, opts),
    refreshTitleVisualsFromCache: (payload) => electron_1.ipcRenderer.invoke("xbox:refresh-title-visuals-cache", payload),
    inspectAuroraGame: (payload) => electron_1.ipcRenderer.invoke("xbox:inspect-aurora-game", payload),
    searchAssets: (payload) => electron_1.ipcRenderer.invoke("xbox:search-assets", payload),
    fetchUrlImage: (url) => electron_1.ipcRenderer.invoke("xbox:fetch-url-image", url),
    chooseAssetImageFile: () => electron_1.ipcRenderer.invoke("xbox:choose-image-file"),
    uploadAssetToConsole: (payload) => electron_1.ipcRenderer.invoke("xbox:upload-asset-to-console", payload),
    getQueue: () => electron_1.ipcRenderer.invoke("xbox:get-queue"),
    removeFromQueue: (game) => electron_1.ipcRenderer.invoke("xbox:remove-queue-item", game),
    getDataStatus: () => electron_1.ipcRenderer.invoke("data:status"),
    clearLocalData: () => electron_1.ipcRenderer.invoke("data:clear"),
    getAria2ListenPort: () => electron_1.ipcRenderer.invoke("config:get-aria2-listen-port"),
    setAria2ListenPort: (v) => electron_1.ipcRenderer.invoke("config:set-aria2-listen-port", v),
    getAria2DhtPort: () => electron_1.ipcRenderer.invoke("config:get-aria2-dht-port"),
    setAria2DhtPort: (v) => electron_1.ipcRenderer.invoke("config:set-aria2-dht-port", v),
    getDefaultXboxDrive: () => electron_1.ipcRenderer.invoke("config:get-default-xbox-drive"),
    setDefaultXboxDrive: (v) => electron_1.ipcRenderer.invoke("config:set-default-xbox-drive", v),
    listXboxDrives: () => electron_1.ipcRenderer.invoke("xbox:list-drives"),
    browseGetGames: (payload) => electron_1.ipcRenderer.invoke("browse:get-games", payload),
    browseFetchCover: (name) => electron_1.ipcRenderer.invoke("browse:fetch-cover", name),
    browseQueueGame: (payload) => electron_1.ipcRenderer.invoke("browse:queue-game", payload),
    browseGetDiscInfo: (game) => electron_1.ipcRenderer.invoke("browse:get-disc-info", game),
    decodeAsset: (payload) => electron_1.ipcRenderer.invoke("xbox:decode-asset", payload),
    encodeAsset: (payload) => electron_1.ipcRenderer.invoke("xbox:encode-asset", payload),
    toolsChooseIsoFiles: () => electron_1.ipcRenderer.invoke("tools:choose-iso-files"),
    toolsChooseOutputFolder: () => electron_1.ipcRenderer.invoke("tools:choose-output-folder"),
    toolsProbeIso: (isoPath) => electron_1.ipcRenderer.invoke("tools:probe-iso", isoPath),
    toolsIso2God: (payload) => electron_1.ipcRenderer.invoke("tools:iso2god", payload),
    toolsIso2Xex: (payload) => electron_1.ipcRenderer.invoke("tools:iso2xex", payload),
    toolsFtpList: (remotePath) => electron_1.ipcRenderer.invoke("tools:ftp-list", remotePath),
    toolsFtpChooseFiles: () => electron_1.ipcRenderer.invoke("tools:ftp-choose-files"),
    toolsFtpChooseFolder: () => electron_1.ipcRenderer.invoke("tools:ftp-choose-folder"),
    toolsFtpUpload: (payload) => electron_1.ipcRenderer.invoke("tools:ftp-upload", payload),
    toolsFtpUploadStatus: () => electron_1.ipcRenderer.invoke("tools:ftp-upload-status"),
    toolsFtpUploadRemove: (id) => electron_1.ipcRenderer.invoke("tools:ftp-upload-remove", id),
    toolsFtpDelete: (remotePath) => electron_1.ipcRenderer.invoke("tools:ftp-delete", remotePath),
    toolsFtpMkdir: (remotePath) => electron_1.ipcRenderer.invoke("tools:ftp-mkdir", remotePath),
    toolsFtpRename: (payload) => electron_1.ipcRenderer.invoke("tools:ftp-rename", payload),
    toolsFtpCopy: (payload) => electron_1.ipcRenderer.invoke("tools:ftp-copy", payload),
    moveGameToDrive: (payload) => electron_1.ipcRenderer.invoke("xbox:move-game", payload),
    onFtpDebugLog: (callback) => {
        const handler = (_event, line) => callback(line);
        electron_1.ipcRenderer.on("godsend-ftp-debug", handler);
        return () => electron_1.ipcRenderer.removeListener("godsend-ftp-debug", handler);
    },
    onFtpProgress: (callback) => {
        const handler = (_event, msg) => callback(msg);
        electron_1.ipcRenderer.on("godsend-ftp-progress", handler);
        return () => electron_1.ipcRenderer.removeListener("godsend-ftp-progress", handler);
    },
    onXboxCover: (callback) => {
        const handler = (_event, data) => callback(data);
        electron_1.ipcRenderer.on("xbox-cover", handler);
        return () => electron_1.ipcRenderer.removeListener("xbox-cover", handler);
    },
    onXboxTitleVisuals: (callback) => {
        const handler = (_event, data) => callback(data);
        electron_1.ipcRenderer.on("xbox-title-visuals", handler);
        return () => electron_1.ipcRenderer.removeListener("xbox-title-visuals", handler);
    },
    onOutput: (callback) => {
        const handler = (_event, line) => callback(line);
        electron_1.ipcRenderer.on("godsend-output", handler);
        return () => electron_1.ipcRenderer.removeListener("godsend-output", handler);
    },
});
