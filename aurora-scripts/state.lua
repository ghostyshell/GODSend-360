-- ==============================
-- GODSend STATE & CONFIGURATION
-- ==============================
-- All symbols are global so every module can read / write them.

-- ── Connection settings ──────────────────────────────────────────────────────
-- BRAIN_IP is overridden at runtime by godsend_config.ini.
-- Edit that file via FTP (ip=x.x.x.x under [Config]) to change the target.
BRAIN_IP        = "192.168.1.228"
PORT            = "8080"
SERVER_BASE     = ""                -- set by initServerURL()
FILES_URL       = ""                -- set by initServerURL()
DOWNLOAD_FOLDER = "Downloads"
CONFIG_FILE     = "godsend_config.ini"

-- ── Mutable operation state ───────────────────────────────────────────────────
absoluteDownloadsPath = ""
gAbortedOperation  = false
gDownloadStartTime = 0
gLastProgressUpdate = 0
gCurrentPart = 0
gTotalParts  = 0
gInstallDrive  = "Hdd1:"
gTransferMode  = "http"   -- "http" or "ftp"
gInstallType   = "god"    -- "god" | "content" (DLC / Disc 2 path) | "xex" (loose folder)

-- ── Helpers ───────────────────────────────────────────────────────────────────

-- Rebuild SERVER_BASE and FILES_URL whenever BRAIN_IP changes.
function initServerURL()
    SERVER_BASE = "http://" .. BRAIN_IP .. ":" .. PORT
    FILES_URL   = SERVER_BASE .. "/files/"
end

-- Read saved server connection values from godsend_config.ini next to the script.
-- Returns ip, port when values are present.
function loadConfig()
    local path = Script.GetBasePath() .. CONFIG_FILE
    local ok, ini = pcall(IniFile.LoadFile, path)
    if not ok or not ini then return nil, nil end
    local ip = ini:ReadValue("Config", "ip", "")
    local port = ini:ReadValue("Config", "port", "")
    if ip and ip ~= "" then return ip, port end
    return nil, nil
end
