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

local function cleanIniValue(v)
    if type(v) ~= "string" then return "" end
    -- Aurora can return trailing NUL/control chars from INI reads.
    v = v:gsub("[%z\1-\31\127]", "")
    v = v:gsub("^%s+", ""):gsub("%s+$", "")
    return v
end

local function readConnectionFromIni(path)
    local ok, ini = pcall(IniFile.LoadFile, path)
    if not ok or not ini then return "", "" end

    -- Preferred keys used by script runtime config.
    local ip = cleanIniValue(ini:ReadValue("Config", "ip", ""))
    local port = cleanIniValue(ini:ReadValue("Config", "port", ""))

    -- Fallback keys used by bundled GODSend.ini.
    if ip == "" then
        ip = cleanIniValue(ini:ReadValue("Settings", "BrainAddress", ""))
    end
    if port == "" then
        port = cleanIniValue(ini:ReadValue("Settings", "BrainPort", ""))
    end

    return ip, port
end

-- Read saved server connection values.
-- Priority:
-- 1) godsend_config.ini ([Config] ip/port)
-- 2) GODSend.ini ([Settings] BrainAddress/BrainPort or [Config] ip/port)
-- Returns ip, port (empty strings when unavailable).
function loadConfig()
    local basePath = Script.GetBasePath()
    local cfgIp, cfgPort = readConnectionFromIni(basePath .. CONFIG_FILE)
    local iniIp, iniPort = readConnectionFromIni(basePath .. "GODSend.ini")

    local ip = cfgIp ~= "" and cfgIp or iniIp
    local port = cfgPort ~= "" and cfgPort or iniPort

    if ip == "" then
        return nil, nil
    end
    if port == "" then
        return ip, nil
    end
    return ip, port
end
