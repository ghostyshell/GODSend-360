const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("godsendApi", {
  getStartupEnabled: () => ipcRenderer.invoke("startup:get"),
  setStartupEnabled: (enabled) => ipcRenderer.invoke("startup:set", enabled),
  getOutputBuffer: () => ipcRenderer.invoke("godsend:get-buffer"),
  startProcess: () => ipcRenderer.invoke("godsend:start"),
  stopProcess: () => ipcRenderer.invoke("godsend:stop"),
  restartProcess: () => ipcRenderer.invoke("godsend:restart"),
  getTransferFolder: () => ipcRenderer.invoke("config:get-transfer-folder"),
  getEffectiveTransferFolder: () => ipcRenderer.invoke("config:get-effective-transfer-folder"),
  setTransferFolder: (folder) => ipcRenderer.invoke("config:set-transfer-folder", folder),
  chooseTransferFolder: () => ipcRenderer.invoke("config:choose-transfer-folder"),
  getArchiveAuth: () => ipcRenderer.invoke("config:get-archive-auth"),
  loginInternetArchive: (payload) => ipcRenderer.invoke("config:ia-login", payload),
  logoutInternetArchive: () => ipcRenderer.invoke("config:ia-logout"),
  getIAConcurrency: () => ipcRenderer.invoke("config:get-ia-concurrency"),
  setIAConcurrency: (v) => ipcRenderer.invoke("config:set-ia-concurrency", v),
  getROMPath: () => ipcRenderer.invoke("config:get-rom-path"),
  setROMPath: (v) => ipcRenderer.invoke("config:set-rom-path", v),
  refreshCache: (platform) => ipcRenderer.invoke("config:cache-refresh", platform),
  onOutput: (callback) => {
    ipcRenderer.on("godsend-output", (_event, line) => callback(line));
  }
});
