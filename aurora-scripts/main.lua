scriptTitle = "GODSend Store"
scriptAuthor = "Nesquin/david12549"
scriptVersion = "6.1"
scriptDescription = "Browse and install Xbox 360, Original, and Digital (XBLA/DLC) - Now with FTP transfer support!"
scriptIcon = "icon\\icon.xur"
scriptPermissions = { "http", "filesystem" }

require("MenuSystem")

-- ==============================
-- CONNECTION SETTINGS
-- ==============================
-- BRAIN_IP is loaded from godsend_config.ini at startup.
-- To change it, edit godsend_config.ini via FTP (set ip=x.x.x.x under [Config]).
local BRAIN_IP        = "192.168.1.228"   -- overridden by godsend_config.ini
local PORT            = "8080"
local SERVER_BASE     = ""                -- built by initServerURL()
local FILES_URL       = ""                -- built by initServerURL()
local DOWNLOAD_FOLDER = "Downloads"
local CONFIG_FILE     = "godsend_config.ini"

-- Rebuild the two URL roots whenever BRAIN_IP changes.
local function initServerURL()
    SERVER_BASE = "http://" .. BRAIN_IP .. ":" .. PORT
    FILES_URL   = SERVER_BASE .. "/files/"
end

-- Read saved IP from godsend_config.ini next to the script.
local function loadConfig()
    local path = Script.GetBasePath() .. CONFIG_FILE
    local ok, ini = pcall(IniFile.LoadFile, path)
    if not ok or not ini then return nil end
    local ip = ini:ReadValue("Config", "ip", "")
    if ip and ip ~= "" then return ip end
    return nil
end


-- ==============================
-- ERROR CODES & TROUBLESHOOTING
-- ==============================
-- Centralized error messages with user-friendly troubleshooting tips
local ErrorHelp = {
    NO_NETWORK = {
        title = "No Network Connection",
        message = "Your Xbox is not connected to the network.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Check your ethernet cable or WiFi adapter\n" ..
            "2. Go to Xbox Settings > Network to test connection\n" ..
            "3. Make sure your router is powered on"
    },
    SERVER_UNREACHABLE = {
        title = "Server Unreachable",
        -- message is a sentinel; showError appends the live SERVER_BASE at call time
        message = "Cannot reach the GODSend server.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Verify the server (godsend.exe) is running on your PC\n" ..
            "2. If the IP is wrong, edit godsend_config.ini via FTP\n" ..
            "   and restart the script  (set ip=x.x.x.x under [Config])\n" ..
            "3. Make sure your PC and Xbox are on the same network\n" ..
            "4. Check your PC firewall allows port 8080\n" ..
            "5. If using Pi-hole/DNS filter, whitelist the server IP"
    },
    DOWNLOAD_FAILED = {
        title = "Download Failed",
        message = "The file download did not complete.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Check your network connection is stable\n" ..
            "2. Try using FTP transfer mode instead of HTTP\n" ..
            "3. Make sure the server is still running\n" ..
            "4. If DashLaunch is installed, disable 'liveblock'\n" ..
            "5. Try restarting Aurora and attempting again"
    },
    DOWNLOAD_TIMEOUT = {
        title = "Download Timed Out",
        message = "The download took too long to start or stalled.\n\n" ..
            "Troubleshooting:\n" ..
            "1. The server may still be processing - try again in a minute\n" ..
            "2. Check that the server PC isn't sleeping or locked\n" ..
            "3. Try FTP transfer mode for more reliable transfers\n" ..
            "4. Check for network congestion on your router"
    },
    MANIFEST_FAILED = {
        title = "Manifest Download Failed",
        message = "Could not download the game index file.\n\n" ..
            "Troubleshooting:\n" ..
            "1. The game may not have finished processing on the server\n" ..
            "2. Check the server console for errors\n" ..
            "3. Try triggering the download again\n" ..
            "4. Game name may contain special characters - check server logs"
    },
    MANIFEST_EMPTY = {
        title = "Empty Game Manifest",
        message = "The game index file exists but contains no download entries.\n\n" ..
            "Troubleshooting:\n" ..
            "1. The game may have failed during server processing\n" ..
            "2. Check the server console for conversion errors\n" ..
            "3. Delete the game from the server's Ready folder and retry"
    },
    MANIFEST_MISSING_IDS = {
        title = "Missing Game IDs",
        message = "The game manifest is missing TitleID or MediaID.\n\n" ..
            "Troubleshooting:\n" ..
            "1. The ISO-to-GOD conversion may have failed\n" ..
            "2. Check the server console for iso2god errors\n" ..
            "3. Delete the game from Ready folder and re-trigger"
    },
    INSTALL_FAILED = {
        title = "Installation Failed",
        message = "Could not extract or install the game files.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Check that your install drive has enough free space\n" ..
            "2. The drive may be corrupted - try a different drive\n" ..
            "3. The downloaded archive may be corrupted\n" ..
            "4. Try the download again - it may have been incomplete"
    },
    DISK_SPACE = {
        title = "Storage Issue",
        message = "There may not be enough space on the target drive.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Check free space on your install drive\n" ..
            "2. Xbox 360 games can be 6-8 GB, ensure enough room\n" ..
            "3. Try installing to a different drive (USB/HDD)\n" ..
            "4. Delete unused games to free up space"
    },
    FTP_REGISTER_FAILED = {
        title = "FTP Registration Failed",
        message = "Could not register your Xbox for FTP transfer.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Make sure Aurora's FTP server is enabled\n" ..
            "2. Check Aurora Settings > Network > Enable FTP\n" ..
            "3. Default FTP port should be 21\n" ..
            "4. Falling back to HTTP transfer mode"
    },
    TRIGGER_FAILED = {
        title = "Could Not Start Download",
        message = "The server did not confirm the download request.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Check the server console for errors\n" ..
            "2. The server may be busy processing another game\n" ..
            "3. Try again in a moment"
    },
    FILE_MOVE_FAILED = {
        title = "File Install Failed",
        message = "Downloaded file could not be moved to install location.\n\n" ..
            "Troubleshooting:\n" ..
            "1. The target drive may be full or read-only\n" ..
            "2. Try a different install drive\n" ..
            "3. Check that the drive is properly formatted (FAT32)\n" ..
            "4. Restart Aurora and try again"
    },
    CANCELLED = {
        title = "Cancelled",
        message = "Operation was cancelled by user."
    },
    HTTP_PARSE_ERROR = {
        title = "Server Response Error",
        message = "Received an unexpected response from the server.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Make sure your GODSend server version matches this script (v6.1)\n" ..
            "2. Restart the server application\n" ..
            "3. Check the server console for error messages"
    }
}

-- Show an error with troubleshooting info
local function showError(errorKey, extraInfo)
    local err = ErrorHelp[errorKey]
    if not err then
        Script.ShowMessageBox("Error", extraInfo or "An unknown error occurred.", "OK")
        return
    end

    local msg = err.message

    -- Append the live server address for connectivity errors so the user
    -- can immediately see which IP the script tried to reach.
    if errorKey == "SERVER_UNREACHABLE" then
        msg = msg .. "\n\nTried: " .. (SERVER_BASE ~= "" and SERVER_BASE or BRAIN_IP .. ":" .. PORT)
    end

    if extraInfo and extraInfo ~= "" then
        msg = msg .. "\n\nDetails: " .. tostring(extraInfo)
    end

    Script.ShowMessageBox(err.title, msg, "OK")
end

-- ==============================
-- GLOBALS
-- ==============================
local absoluteDownloadsPath = ""
gAbortedOperation = false
gDownloadStartTime = 0
gLastProgressUpdate = 0
gCurrentPart = 0
gTotalParts = 0
gInstallDrive = "Hdd1:" 
gTransferMode = "http"  -- "http" or "ftp"

-- ==============================
-- UTILITY FUNCTIONS
-- ==============================

local function getTime()
    local ok, t = pcall(Aurora.GetTime)
    if ok and t then 
        return (t.Hour or 0) * 3600 + (t.Minute or 0) * 60 + (t.Second or 0) 
    end
    return 0
end

local function formatSize(bytes)
    if not bytes or bytes < 0 then return "0 KB" end
    if bytes >= 1073741824 then
        return string.format("%.2f GB", bytes / 1073741824)
    elseif bytes >= 1048576 then
        return string.format("%.2f MB", bytes / 1048576)
    else
        return string.format("%.2f KB", bytes / 1024)
    end
end

-- formatDuration formats seconds as "1m23s" or "45s"
local function formatDuration(secs)
    if not secs or secs < 0 then secs = 0 end
    secs = math.floor(secs)
    if secs < 60 then
        return secs .. "s"
    end
    return math.floor(secs / 60) .. "m" .. string.format("%02d", secs % 60) .. "s"
end

local function httpGet(url)
    local ok, r = pcall(Http.Get, url)
    if not ok then return nil, "HTTP request threw an error" end
    if r and r.Success then return r.OutputData, nil end
    if r and r.StatusCode then
        return nil, "HTTP " .. tostring(r.StatusCode)
    end
    return nil, "No response from server"
end

local function sanitizeForUrl(name)
    if not name then return "" end
    return name:gsub('[<>:"/\\|%?%*]', " -")
end

-- Get the Xbox's IP address for FTP registration
local function getXboxIP()
    local ok, ip = pcall(Aurora.GetIPAddress)
    if ok and ip and ip ~= "" then
        return ip
    end
    return "0.0.0.0"
end

-- Safe JSON field extraction with pcall protection
local function jsonField(json, field)
    if not json or type(json) ~= "string" then return nil end
    local ok, result = pcall(function()
        return json:match('"' .. field .. '"%s*:%s*"([^"]*)"')
    end)
    if ok then return result end
    return nil
end

-- Validate server response looks like valid JSON/text (not HTML error page)
local function validateResponse(data)
    if not data then return false end
    if type(data) ~= "string" then return false end
    if data:len() == 0 then return false end
    -- Check for HTML error pages (server returned a web error)
    if data:sub(1, 1) == "<" and data:find("<html") then return false end
    return true
end

-- ==============================
-- HTTP PROGRESS CALLBACK
-- ==============================

function HttpProgressRoutine(dwTotalFileSize, dwTotalBytesTransferred, dwReason)
    -- Wrap everything in pcall to prevent any crash in the callback
    local ok, result = pcall(function()
        if Script.IsCanceled() then
            gAbortedOperation = true
            return 1
        end
        
        -- Guard against nil or invalid values
        local totalSize = dwTotalFileSize or 0
        local transferred = dwTotalBytesTransferred or 0
        
        Script.SetProgress(transferred, totalSize)

        local now = getTime()
        -- Update text every second to prevent flickering
        if now > gLastProgressUpdate then
            local elapsed = now - gDownloadStartTime
            if elapsed < 1 then elapsed = 1 end
            
            local percent = 0
            if totalSize > 0 then
                percent = math.floor((transferred / totalSize) * 100)
                -- Clamp to valid range
                if percent > 100 then percent = 100 end
                if percent < 0 then percent = 0 end
            end

            local speedBytes = transferred / elapsed
            local speedStr = formatSize(speedBytes) .. "/s"
            local downloadedStr = formatSize(transferred)
            local elapsedStr = formatDuration(elapsed)

            -- ETA: remaining bytes / speed
            local etaStr = ""
            if totalSize > 0 and speedBytes > 0 and percent < 100 then
                local remaining = (totalSize - transferred) / speedBytes
                etaStr = " | ~" .. formatDuration(remaining) .. " left"
            end

            local status = ""
            if gTotalParts > 1 then
                status = string.format("Part %d/%d: %d%% | %s\n%s | %s%s",
                    gCurrentPart, gTotalParts, percent, downloadedStr, speedStr, elapsedStr, etaStr)
            else
                status = string.format("Downloading: %d%% | %s\n%s | %s%s",
                    percent, downloadedStr, speedStr, elapsedStr, etaStr)
            end

            Script.SetStatus(status)
            gLastProgressUpdate = now
        end
        return 0
    end)
    
    if not ok then
        -- If the progress callback crashes, abort gracefully instead of crashing Aurora
        gAbortedOperation = true
        return 1
    end
    
    return result or 0
end

-- ==============================
-- SERVER COMMUNICATION
-- ==============================

local function getGameStatus(gameName)
    if not gameName or gameName == "" then
        return "Error", "Invalid game name"
    end
    
    local encodedName = Http.UrlEncode(gameName)
    if not encodedName then
        return "Error", "Failed to encode game name"
    end
    
    local url = SERVER_BASE .. "/status?game=" .. encodedName
    local json, err = httpGet(url)
    
    if not json then
        return "Error", err or "No Response"
    end
    
    if not validateResponse(json) then
        return "Error", "Invalid server response"
    end
    
    local state = jsonField(json, "state")
    local message = jsonField(json, "message")
    
    if state then
        return state, message or ""
    end
    
    return "Error", "Could not parse server response"
end

local function triggerDownload(gameName, platform)
    if not gameName or gameName == "" then
        showError("TRIGGER_FAILED", "No game name provided")
        return false
    end
    
    local encodedName = Http.UrlEncode(gameName)
    if not encodedName then
        showError("TRIGGER_FAILED", "Failed to encode game name")
        return false
    end
    
    local url = SERVER_BASE .. "/trigger?game=" .. encodedName .. "&platform=" .. (platform or "xbox360")
    local json, err = httpGet(url)
    
    if not json then
        showError("TRIGGER_FAILED", err)
        return false
    end
    
    if not validateResponse(json) then
        showError("HTTP_PARSE_ERROR")
        return false
    end
    
    if json:find("triggered") or json:find("already_ready") then
        return true
    end

    -- Local-only mode: server does not use Internet Archive
    if json:find("local_unavailable") then
        local msg = json:match('"message"%s*:%s*"([^"]*)"')
            or "No ISO found in the PC Transfer folder."
        Script.ShowMessageBox("Local Transfer", msg, "OK")
        return false
    end
    
    showError("TRIGGER_FAILED", "Server response: " .. json:sub(1, 100))
    return false
end

-- Register Xbox for FTP transfer with server
local function registerForFTP(gameName, platform)
    local xboxIP = getXboxIP()
    
    if xboxIP == "0.0.0.0" then
        showError("FTP_REGISTER_FAILED", "Could not detect Xbox IP address")
        return false
    end
    
    local encodedName = Http.UrlEncode(gameName)
    if not encodedName then
        showError("FTP_REGISTER_FAILED", "Failed to encode game name")
        return false
    end
    
    local url = SERVER_BASE .. "/register?game=" .. encodedName
        .. "&ip=" .. Http.UrlEncode(xboxIP)
        .. "&drive=" .. Http.UrlEncode(gInstallDrive)
        .. "&platform=" .. (platform or "xbox360")
        .. "&mode=" .. gTransferMode
    
    local json, err = httpGet(url)
    
    if not json then
        showError("FTP_REGISTER_FAILED", err)
        return false
    end
    
    if json:find("registered") then
        return true
    end
    
    showError("FTP_REGISTER_FAILED", "Unexpected server response")
    return false
end

-- ==============================
-- CONNECTION TEST
-- ==============================

local function testServerConnection()
    Script.SetStatus("Testing server connection...")
    local json, err = httpGet(SERVER_BASE .. "/status?game=__ping__")
    if not json then
        showError("SERVER_UNREACHABLE", err)
        return false
    end
    return true
end

-- ==============================
-- WAIT FOR PROCESSING
-- ==============================

-- waitForProcessing polls the server until the job is Ready or fails.
-- Returns:
--   true           — job finished, proceed to install
--   false          — error or user chose to abort
--   "backgrounded" — user dismissed the window; server keeps running
local function waitForProcessing(gameName)
    Script.ShowNotification("Initializing...")
    Thread.Sleep(2000)

    local dotCount = 0
    local failCount = 0
    local maxFails = 15  -- 15 consecutive failures = ~30 seconds of no response

    while true do
        -- Check for user pressing B / cancel
        if Script.IsCanceled() then
            -- Fetch the live state so we can give an accurate prompt
            local liveState, liveMsg = getGameStatus(gameName)
            liveMsg = liveMsg or ""

            local promptTitle = "Transfer Still Running"
            local promptBody

            if gTransferMode == "ftp" then
                promptBody =
                    "The server is still transferring '" .. gameName .. "' via FTP.\n\n" ..
                    "  Status: " .. liveMsg .. "\n\n" ..
                    "Background — the FTP transfer continues automatically.\n" ..
                    "  Your game will appear in Aurora when it finishes.\n\n" ..
                    "Abort — return to menu (transfer still runs, install later)."
            else
                promptBody =
                    "The server is still processing '" .. gameName .. "'.\n\n" ..
                    "  Status: " .. liveMsg .. "\n\n" ..
                    "Background — go back to the menu now.\n" ..
                    "  When ready, select the game again from the library\n" ..
                    "  to install it (or use Server Queue & Status).\n\n" ..
                    "Abort — same as Background (server keeps going)."
            end

            local choice = Script.ShowMessageBox(promptTitle, promptBody,
                                                 "Background", "Abort")

            -- Button 1 = Background, Button 2 = Abort
            -- In both cases the server keeps running; only the return value differs
            -- so the caller can show the right follow-up message.
            if choice and choice.Button == 1 then
                return "backgrounded"
            else
                return false
            end
        end

        collectgarbage()

        local state, message = getGameStatus(gameName)
        local dots = string.rep(".", dotCount % 4)

        if state == "Ready" then
            if gTransferMode == "ftp" then
                Script.ShowNotification("FTP Transfer Complete!")
            else
                Script.ShowNotification("Download Ready!")
            end
            return true

        elseif state == "Processing" then
            local msg = message or "Processing"
            Script.SetStatus("Host:\n" .. msg .. dots)

            -- Parse percentage from messages like "FTP: 15/32 (46.8%)" or "Downloading: 75%"
            local pct = msg:match("%((%d+%.?%d*)%%%)") -- "(46.8%)" style
                     or msg:match(":%s*(%d+)%%")        -- ": 75%" style
            pct = tonumber(pct)
            if pct then
                Script.SetProgress(math.floor(pct), 100)
            else
                Script.SetProgress(-1)
            end

            dotCount = dotCount + 1
            failCount = 0

        elseif state == "Error" then
            showError("DOWNLOAD_FAILED", message or "Processing failed on server")
            return false

        else
            -- "Missing" or network hiccup
            failCount = failCount + 1
            if failCount >= maxFails then
                showError("DOWNLOAD_TIMEOUT",
                    "Lost contact with server after " .. (failCount * 2) .. " seconds")
                return false
            end
            Script.SetStatus("Waiting for Host" .. dots ..
                             " (" .. failCount .. "/" .. maxFails .. ")")
            Script.SetProgress(-1)
            dotCount = dotCount + 1
        end

        Thread.Sleep(2000)
    end
end

-- ==============================
-- EXTRACTION LOGIC
-- ==============================

local function extractZipNative(zipPath, destFolder)
    local ok, result, errMsg = pcall(function()
        local basePath = Script.GetBasePath()
        local relativePath = zipPath:gsub("^" .. basePath:gsub("\\", "\\\\"), "")

        local zip = ZipFile.OpenFile(relativePath)
        if not zip then return false, "Could not open archive" end

        local tempExtract = DOWNLOAD_FOLDER .. "\\TempExtract"
        local tempAbs = basePath .. tempExtract

        if zip.Extract(zip, tempExtract .. "\\") then
            local moved = FileSystem.MoveDirectory(tempAbs .. "\\", destFolder, true)
            FileSystem.DeleteDirectory(tempAbs)
            if not moved then
                return false, "Failed to move extracted files to install location"
            end
            return true, nil
        end
        return false, "Archive extraction failed - file may be corrupted"
    end)
    
    if not ok then
        return false, "Extraction crashed: " .. tostring(result)
    end
    
    return result, errMsg
end

-- ==============================
-- MANIFEST & INSTALLATION
-- ==============================

local function parseManifest(iniPath, gameName)
    local ini = IniFile.LoadFile(iniPath)
    if not ini then return nil, nil, nil, nil, nil end

    local titleID   = ini:ReadValue(gameName, "titleid", "")
    local mediaID   = ini:ReadValue(gameName, "mediaid", "")
    -- titlename is set by the server after XboxUnity lookup; may be ""
    local titleName = ini:ReadValue(gameName, "titlename", "")
    local parts = {}
    local dlcs  = {}

    local p1 = ini:ReadValue(gameName, "dataurl", "")
    if p1 ~= "" then table.insert(parts, p1) end

    local i = 2
    while true do
        local p = ini:ReadValue(gameName, "dataurlpart" .. i, "")
        if p == "" then break end
        table.insert(parts, p)
        i = i + 1
    end

    i = 1
    while true do
        local d = ini:ReadValue(gameName, "dlc_" .. i, "")
        if d == "" then break end
        table.insert(dlcs, d)
        i = i + 1
    end

    return parts, titleID, mediaID, dlcs, titleName
end

local function installGame(gameName)
    -- For FTP mode, the server handles everything
    if gTransferMode == "ftp" then
        Script.SetStatus("Server is transferring via FTP...")
        Script.SetProgress(-1)
        Script.ShowMessageBox("Installation Complete",
            "Game has been transferred via FTP.\n\n" ..
            "Go to Settings > Content > Scan to refresh\n" ..
            "your game library.", "OK")
        return
    end
    
    -- HTTP MODE: Download logic with full error handling
    Script.SetStatus("Fetching Manifest...")

    local safeName = sanitizeForUrl(gameName)
    if not safeName or safeName == "" then
        showError("MANIFEST_FAILED", "Game name could not be sanitized")
        return
    end
    
    local gameBaseURL = FILES_URL .. Http.UrlEncode(safeName) .. "/"
    
    -- Download manifest
    local iniUrl = gameBaseURL .. "godsend.ini"
    local localIniRel = DOWNLOAD_FOLDER .. "\\godsend.ini"
    
    local ok, res = pcall(Http.GetEx, iniUrl, function(a,b,c) return 0 end, localIniRel)
    
    if not ok or not res then
        showError("MANIFEST_FAILED", "URL: " .. iniUrl)
        return
    end

    local ini = IniFile.LoadFile(localIniRel)
    if not ini then
        showError("MANIFEST_FAILED", "Downloaded manifest file could not be parsed")
        pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
        return
    end
    
    local installType = ini:ReadValue(gameName, "type", "god")

    -- === PATH 0: XEX FOLDER INSTALL ===
    if installType == "xex" then
        local folderName = ini:ReadValue(gameName, "foldername", gameName)
        local xexPart    = ini:ReadValue(gameName, "dataurl", ""):gsub("%%20", " "):gsub("%%28", "("):gsub("%%29", ")")

        if xexPart == "" then
            showError("MANIFEST_EMPTY", "XEX manifest missing dataurl")
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end

        local installPath = gInstallDrive .. "\\XEX\\" .. folderName .. "\\"
        local mkOk = pcall(FileSystem.CreateDirectory, installPath)
        if not mkOk then
            showError("INSTALL_FAILED", "Could not create XEX directory: " .. installPath)
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end

        local dlRel = DOWNLOAD_FOLDER .. "\\xex_part.7z"
        local dlAbs = absoluteDownloadsPath .. "xex_part.7z"
        local partUrl = gameBaseURL .. Http.UrlEncode(xexPart)

        gAbortedOperation = false
        gDownloadStartTime = getTime()
        gLastProgressUpdate = 0
        gCurrentPart = 1
        gTotalParts = 1

        Script.SetStatus("Downloading XEX...")
        local dlOk, dlRes = pcall(Http.GetEx, partUrl, HttpProgressRoutine, dlRel)

        if gAbortedOperation then
            showError("CANCELLED")
            pcall(FileSystem.DeleteFile, dlAbs)
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end
        if not dlOk or not dlRes then
            showError("DOWNLOAD_FAILED", "XEX archive: " .. partUrl)
            pcall(FileSystem.DeleteFile, dlAbs)
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end

        Script.SetStatus("Extracting XEX to " .. installPath)
        local extractOk, extractErr = extractZipNative(dlRel, installPath)
        pcall(FileSystem.DeleteFile, dlAbs)
        if not extractOk then
            showError("INSTALL_FAILED", extractErr)
        else
            Script.ShowMessageBox("Installation Complete",
                "XEX game installed to:\n" .. installPath .. "\n\n" ..
                "Go to Settings > Content > Scan to detect it\n" ..
                "or add the XEX folder to Aurora's scan paths.", "OK")
        end
        pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
        return
    end

    -- === PATH 1: DIGITAL/XBLA/DLC (RAW) INSTALL ===
    if installType == "raw" then
        local rawFile = ini:ReadValue(gameName, "filename", ""):gsub("%s+", "")
        local relPath = ini:ReadValue(gameName, "path", ""):gsub("%s+", "")
        -- Server may force a drive (e.g. Hdd1 for DLC/XBLA)
        local forcedDrive = ini:ReadValue(gameName, "drive", ""):gsub("%s+", "")
        
        if rawFile == "" or relPath == "" then
            showError("MANIFEST_EMPTY", "Raw manifest missing filename or path")
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end
        
        local dlcDrive = (forcedDrive ~= "") and forcedDrive or "Hdd1:"
        -- Create directory structure safely
        local currentPath = dlcDrive .. "\\"
        for folder in relPath:gmatch("[^\\]+") do
            currentPath = currentPath .. folder .. "\\"
            local mkOk = pcall(FileSystem.CreateDirectory, currentPath)
            if not mkOk then
                showError("INSTALL_FAILED", "Could not create directory: " .. currentPath)
                pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
                return
            end
        end
        local fullInstallPath = dlcDrive .. "\\" .. relPath
        
        local safeTempName = "temp_raw.bin"
        local tempRawRel = DOWNLOAD_FOLDER .. "\\" .. safeTempName
        local tempRawAbs = absoluteDownloadsPath .. safeTempName
        local destAbs = fullInstallPath .. rawFile
        
        -- Clean up any previous temp file
        pcall(FileSystem.DeleteFile, tempRawAbs)
        
        -- Set Globals for Progress Routine
        gCurrentPart = 1
        gTotalParts = 1
        gAbortedOperation = false
        gDownloadStartTime = getTime()
        gLastProgressUpdate = 0
        
        local downloadUrl = gameBaseURL .. rawFile
        Script.SetStatus("Downloading " .. rawFile)
        
        local dlOk, dlRes = pcall(Http.GetEx, downloadUrl, HttpProgressRoutine, tempRawRel)
        
        if gAbortedOperation then
            showError("CANCELLED")
            pcall(FileSystem.DeleteFile, tempRawAbs)
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end
        
        if not dlOk or not dlRes then
            showError("DOWNLOAD_FAILED", "File: " .. rawFile .. "\nURL: " .. downloadUrl)
            pcall(FileSystem.DeleteFile, tempRawAbs)
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end
        
        Script.SetStatus("Finalizing...")
        Thread.Sleep(500)
        
        if not FileSystem.FileExists(tempRawAbs) then
            showError("DOWNLOAD_FAILED", "File disappeared after download - possible disk issue")
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end

        pcall(FileSystem.DeleteFile, destAbs)
        
        local success = false
        local moveError = ""
        
        -- Try rename first (fast, same-drive move)
        local renameOk = pcall(function()
            if FileSystem.Rename(tempRawAbs, destAbs) then
                success = true
            end
        end)
        
        -- Fall back to copy if rename fails (cross-drive)
        if not success then
            local copyOk = pcall(function()
                if FileSystem.CopyFile(tempRawAbs, destAbs, function() end) then
                    success = true
                    FileSystem.DeleteFile(tempRawAbs)
                else
                    moveError = "Copy operation returned false"
                end
            end)
            if not copyOk then
                moveError = "Copy operation threw an error"
            end
        end
        
        if success then
            Script.ShowMessageBox("Installation Complete",
                "Game installed successfully!\n\n" ..
                "Go to Settings > Content > Scan to refresh\n" ..
                "your game library.", "OK")
        else
            showError("FILE_MOVE_FAILED", moveError)
        end
        
        pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
        return
    end

    -- === PATH 2: STANDARD (GOD) INSTALL ===
    local downloadQueue, titleID, mediaID, dlcs, titleName = parseManifest(localIniRel, gameName)
    if not downloadQueue or #downloadQueue == 0 then
        showError("MANIFEST_EMPTY")
        pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
        return
    end

    if not titleID or titleID == "" or not mediaID or mediaID == "" then
        showError("MANIFEST_MISSING_IDS")
        pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
        return
    end

    -- Use the XboxUnity-resolved title name; fall back to "Title" if not available.
    local godPrefix = (titleName and titleName ~= "") and titleName or "Title"
    local installPath = gInstallDrive .. "\\GOD\\" .. godPrefix .. " - " .. titleID .. "\\" .. mediaID .. "\\"
    local mkOk = pcall(FileSystem.CreateDirectory, installPath)
    if not mkOk then
        showError("INSTALL_FAILED", "Could not create install directory on " .. gInstallDrive)
        pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
        return
    end
    
    -- Set Global for Progress Routine
    gTotalParts = #downloadQueue

    for i, urlFrag in ipairs(downloadQueue) do
        local fullUrl = gameBaseURL .. urlFrag
        local fileName = "part" .. i .. ".7z"
        local dlRel = DOWNLOAD_FOLDER .. "\\" .. fileName
        local dlAbs = absoluteDownloadsPath .. fileName

        gAbortedOperation = false
        gDownloadStartTime = getTime()
        gLastProgressUpdate = 0
        gCurrentPart = i
        collectgarbage()

        Script.SetStatus("Starting Part " .. i .. " / " .. gTotalParts)
        
        local dlOk, dlRes = pcall(Http.GetEx, fullUrl, HttpProgressRoutine, dlRel)

        if gAbortedOperation then
            showError("CANCELLED")
            pcall(FileSystem.DeleteFile, dlAbs)
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end
        
        if not dlOk then
            showError("DOWNLOAD_FAILED", 
                "Part " .. i .. "/" .. gTotalParts .. " crashed during download.\n" ..
                "File: " .. fileName .. "\n" ..
                "Error: " .. tostring(dlRes) .. "\n\n" ..
                "This may be caused by a network interruption or\n" ..
                "memory issue. Try FTP mode for more reliable transfers.")
            pcall(FileSystem.DeleteFile, dlAbs)
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end
        
        if not dlRes then
            showError("DOWNLOAD_FAILED", 
                "Part " .. i .. "/" .. gTotalParts .. " failed.\n" ..
                "File: " .. fileName .. "\nURL: " .. fullUrl)
            pcall(FileSystem.DeleteFile, dlAbs)
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end

        Script.SetStatus("Installing Part " .. i .. "...")
        local extractOk, extractErr = extractZipNative(dlRel, installPath)
        
        -- Clean up downloaded archive regardless of extract result
        pcall(FileSystem.DeleteFile, dlAbs)
        collectgarbage()

        if not extractOk then
            showError("INSTALL_FAILED", 
                "Part " .. i .. "/" .. gTotalParts .. " extraction failed.\n" ..
                (extractErr or "Unknown extraction error"))
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end
    end
    
    -- === 3. DLC INSTALL — always to Hdd1 ===
    if dlcs and #dlcs > 0 then
        local dlcPath = "Hdd1:\\Content\\0000000000000000\\" .. titleID .. "\\00000002\\"
        pcall(FileSystem.CreateDirectory, dlcPath)
        
        gTotalParts = #dlcs
        local dlcFailures = {}
        
        for i, dlcUrlFrag in ipairs(dlcs) do
            local dlcUrlFull = gameBaseURL .. dlcUrlFrag
            local dlcArchiveName = "dlc_temp_" .. i .. ".7z"
            local dlcRel = DOWNLOAD_FOLDER .. "\\" .. dlcArchiveName
            local dlcAbs = absoluteDownloadsPath .. dlcArchiveName
            
            gAbortedOperation = false
            gDownloadStartTime = getTime()
            gLastProgressUpdate = 0
            gCurrentPart = i
            
            Script.SetStatus("Starting DLC " .. i .. " / " .. #dlcs)
            
            local dlcOk, dlcRes = pcall(Http.GetEx, dlcUrlFull, HttpProgressRoutine, dlcRel)
            
            if gAbortedOperation then
                showError("CANCELLED")
                pcall(FileSystem.DeleteFile, dlcAbs)
                break
            end
            
            if dlcOk and dlcRes then
                Script.SetStatus("Installing DLC " .. i .. "...")
                local extOk, extErr = extractZipNative(dlcRel, dlcPath)
                if not extOk then
                    table.insert(dlcFailures, "DLC " .. i .. ": " .. (extErr or "extract failed"))
                end
                pcall(FileSystem.DeleteFile, dlcAbs)
            else
                table.insert(dlcFailures, "DLC " .. i .. ": download failed")
                pcall(FileSystem.DeleteFile, dlcAbs)
            end
            collectgarbage()
        end
        
        -- Report DLC failures but don't block the main game install
        if #dlcFailures > 0 then
            local failMsg = "Game installed OK, but some DLC failed:\n\n"
            for _, f in ipairs(dlcFailures) do
                failMsg = failMsg .. "- " .. f .. "\n"
            end
            failMsg = failMsg .. "\nYou can retry DLC later."
            Script.ShowMessageBox("DLC Warning", failMsg, "OK")
        end
    end

    Script.ShowMessageBox("Installation Complete",
        "Game installed successfully!\n\n" ..
        "Go to Settings > Content > Scan to refresh\n" ..
        "your game library.", "OK")
    pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
end

-- ==============================
-- QUEUE / STATUS VIEWER
-- ==============================

-- Parse a flat JSON array returned by /queue.
-- Handles the simple structure: [{"game":"...","state":"...","message":"..."},...]
-- parseQueueJSON parses the flat JSON array returned by /queue.
-- Uses a simple field-by-field approach that is robust to message content.
local function parseQueueJSON(json)
    local entries = {}
    if not json or json == "" or json == "[]" then return entries end

    -- Walk the raw JSON string finding each field value directly.
    -- We avoid {[^}]+} because a message could theoretically contain }.
    -- Instead we find each "game","state","message" triple in sequence.
    local pos = 1
    while true do
        -- Advance to next object start
        local objStart = json:find("{", pos, true)
        if not objStart then break end

        local game  = json:match('"game"%s*:%s*"([^"]*)"',    objStart)
        local state = json:match('"state"%s*:%s*"([^"]*)"',   objStart)
        local msg   = json:match('"message"%s*:%s*"([^"]*)"', objStart)

        if game and game ~= "" and state and state ~= "" then
            table.insert(entries, {
                game    = game,
                state   = state,
                message = msg or ""
            })
        end

        -- Advance past this object
        local objEnd = json:find("}", objStart, true)
        pos = objEnd and (objEnd + 1) or (#json + 1)
    end
    return entries
end

-- State icon prefix for the popup list (ASCII only — no UTF-8)
local stateIcon = {
    Processing = " >> ",
    Ready      = " OK  ",
    Error      = " !! ",
    Missing    = "  ?  "
}

-- Format one entry for the popup list row.
-- Uses "..." instead of the UTF-8 ellipsis which crashes Aurora's Lua 5.1.
local function formatQueueEntry(e)
    local icon  = stateIcon[e.state] or "  ?  "
    local label = "[" .. icon .. "] " .. (e.game or "?")
    local msg   = e.message or ""
    if msg ~= "" then
        -- Trim to 35 bytes (Lua 5.1 # counts bytes; safe for ASCII server msgs)
        local short = msg:sub(1, 35)
        if #msg > 35 then short = short .. "..." end  -- ASCII dots, not UTF-8 ellipsis
        label = label .. "  |  " .. short
    end
    return label
end

-- Build the detail string shown when the user clicks a job entry.
local function jobDetail(e)
    local game  = e.game    or "?"
    local state = e.state   or "?"
    local msg   = e.message or ""

    -- Extract a percentage like "(46.8%)" or ": 75%"
    local pct = msg:match("%((%d+%.?%d*)%%%)") -- "(46.8%)" style
             or msg:match(":%s*(%d+)%%")        -- ": 75%"  style
    local bar = ""
    if pct then
        -- Clamp n to [0,20] to avoid negative string.rep on Lua 5.1
        local n = math.floor(tonumber(pct) / 5)
        if n < 0  then n = 0  end
        if n > 20 then n = 20 end
        bar = "\n[" .. string.rep("=", n) .. string.rep("-", 20 - n) .. "] " .. pct .. "%"
    end

    return "Game    : " .. game  .. "\n" ..
           "State   : " .. state .. "\n" ..
           "Message : " .. msg   .. bar
end

function showQueue()
    while true do
        collectgarbage()
        Script.SetStatus("Fetching server queue...")

        -- Fetch job list
        local data, err = httpGet(SERVER_BASE .. "/queue")
        if not data then
            Script.ShowMessageBox("Queue Error",
                "Could not reach the server.\n\n" .. tostring(err or "Unknown error"), "OK")
            return
        end

        local entries = parseQueueJSON(data)

        -- Fetch cache status (best-effort, don't crash if unavailable)
        local cacheReady, cacheBuilding = 0, 0
        local cacheLines = {}
        local cacheOk, cacheData = pcall(httpGet, SERVER_BASE .. "/cache-status")
        if cacheOk and cacheData then
            for st in cacheData:gmatch('"state"%s*:%s*"([^"]*)"') do
                if st == "ready"    then cacheReady    = cacheReady    + 1 end
                if st == "building" then cacheBuilding = cacheBuilding + 1 end
            end
            -- Build per-platform lines for the detail view
            for platform, st, loaded, total, games in
                cacheData:gmatch('"(%w+)"%s*:%s*%b{}') do
                -- Simple extraction inside each platform block
                local block = cacheData:match('"' .. platform .. '"%s*:%s*(%b{})')
                if block then
                    local bst = block:match('"state"%s*:%s*"([^"]*)"') or "?"
                    local bld = block:match('"loaded"%s*:%s*(%d+)') or "0"
                    local btl = block:match('"total"%s*:%s*(%d+)')  or "0"
                    local bgm = block:match('"games"%s*:%s*(%d+)')  or "0"
                    table.insert(cacheLines,
                        string.format("%-8s %s %s/%s (%s games)", platform, bst, bld, btl, bgm))
                end
            end
        end

        -- Build display list: jobs, then Clear all, Cache, Refresh
        local displayList = {}
        if #entries == 0 then
            table.insert(displayList, "  (No active jobs on server)")
        else
            for _, e in ipairs(entries) do
                table.insert(displayList, formatQueueEntry(e))
            end
        end

        table.insert(displayList, "  !! Clear ALL server jobs")
        local clearAllRow = #displayList

        local cacheRow = string.format(
            "  [Cache] %d platforms ready, %d building", cacheReady, cacheBuilding)
        table.insert(displayList, cacheRow)
        local cacheRowIdx = #displayList

        table.insert(displayList, "  >> Refresh")
        local refreshRow = #displayList

        -- Title
        local proc = 0
        for _, e in ipairs(entries) do
            if e.state == "Processing" then proc = proc + 1 end
        end
        local title = (#entries > 0)
            and string.format("Server Status  (%d jobs, %d active)", #entries, proc)
            or  "Server Status  (idle)"

        local r = Script.ShowPopupList(title, "B=Back  A=Select", displayList)
        if not r or r.Canceled then return end

        local sel = r.Selected and r.Selected.Key
        if not sel then return end  -- safety: nil key means dialog dismissed

        if sel == refreshRow then
            -- loop to refresh

        elseif sel == cacheRowIdx then
            -- Show cache detail
            local detail = (#cacheLines > 0)
                and table.concat(cacheLines, "\n")
                or  (cacheOk and cacheData or "Cache status unavailable")
            pcall(Script.ShowMessageBox, "Cache Status", tostring(detail):sub(1, 500), "OK")

        elseif sel == clearAllRow then
            local rm, rmErr = httpGet(SERVER_BASE .. "/queue/remove")
            if rm and (rm:find("cleared") or rm:find("removed")) then
                Script.ShowNotification("Server queue cleared")
            elseif rm then
                Script.ShowMessageBox("Queue", "Server: " .. rm:sub(1, 120), "OK")
            else
                Script.ShowMessageBox("Queue", "Could not clear: " .. tostring(rmErr or "?"), "OK")
            end

        elseif #entries == 0 and sel == 1 then
            -- Placeholder row only — refresh

        elseif sel >= 1 and sel <= #entries and entries[sel] then
            local e = entries[sel]
            local sub = Script.ShowPopupList(e.game, "A=Select", {
                "View details",
                "Remove from queue",
                "Cancel",
            })
            if not sub or sub.Canceled then
                -- back
            elseif sub.Selected and sub.Selected.Key == 1 then
                local ok, errMsg = pcall(function()
                    Script.ShowMessageBox(e.game, jobDetail(e), "OK")
                end)
                if not ok then
                    Script.ShowMessageBox("Detail",
                        "Game: " .. tostring(e.game) ..
                        "\nState: " .. tostring(e.state) ..
                        "\nMsg: " .. tostring(e.message), "OK")
                end
            elseif sub.Selected and sub.Selected.Key == 2 then
                local enc = Http.UrlEncode(e.game)
                if enc then
                    local rm, rmErr = httpGet(SERVER_BASE .. "/queue/remove?game=" .. enc)
                    if rm and (rm:find("removed") or rm:find("status")) then
                        Script.ShowNotification("Removed: " .. e.game)
                    elseif rm then
                        Script.ShowMessageBox("Remove", rm:sub(1, 120), "OK")
                    else
                        Script.ShowMessageBox("Remove failed", tostring(rmErr or "?"), "OK")
                    end
                end
            end
        end
    end
end

-- ==============================
-- MENU LOGIC
-- ==============================

function browseLibrary(platform)
    Script.SetStatus("Loading Library...")
    local list_data, err = httpGet(SERVER_BASE .. "/browse?platform=" .. platform)
    collectgarbage()

    if list_data then
        -- Handle IA cache loading marker (format: __IA_LOADING__:loaded/total)
        if list_data:sub(1, 14) == "__IA_LOADING__" then
            local loaded, total = list_data:match("__IA_LOADING__:(%d+)/(%d+)")
            local progressMsg = ""
            if loaded and total then
                progressMsg = string.format("\nProgress: %s / %s collections fetched.", loaded, total)
            end
            Script.ShowMessageBox("Library Loading",
                "The Internet Archive game list is still loading\n" ..
                "(fetching from archive.org for the first time).\n" ..
                progressMsg .. "\n\n" ..
                "Please go back and try again in a moment.\n" ..
                "After the first load, the list is saved to disk.", "OK")
            return
        end

        if not validateResponse(list_data) then
            showError("HTTP_PARSE_ERROR", "Browse returned invalid data")
            return
        end
        
        local buckets = {}
        local bucketKeys = {}
        
        for game in list_data:gmatch("([^|]+)") do 
            local firstChar = string.upper(string.sub(game, 1, 1))
            if string.match(firstChar, "%d") then firstChar = "#" end
            
            if not buckets[firstChar] then
                buckets[firstChar] = {}
                table.insert(bucketKeys, firstChar)
            end
            table.insert(buckets[firstChar], game)
        end

        if #bucketKeys == 0 then
            local emptyMsg = "No games found in this library."
            if platform == "local" then
                emptyMsg = "No ISO files found in the Transfer folder.\n\n" ..
                    "Place your .iso files in the Transfer folder on\n" ..
                    "your PC next to godsend.exe, then try again."
            else
                emptyMsg = "No games found in the Internet Archive library.\n\n" ..
                    "The game list may still be loading (first launch\n" ..
                    "takes ~60 seconds to fetch from archive.org).\n\n" ..
                    "Go back and try again in a moment.\n" ..
                    "Check the server console for connection errors."
            end
            Script.ShowMessageBox("Empty Library", emptyMsg, "OK")
            return
        end

        table.sort(bucketKeys, function(a, b)
            if a == "#" then return true end
            if b == "#" then return false end
            return a < b
        end)

        local title = "Xbox 360 Redump (Internet Archive)"
        if platform == "xbox"    then title = "Original Xbox Redump (Internet Archive)" end
        if platform == "digital" then title = "Digital / No-Intro (Internet Archive)" end
        if platform == "xbla"    then title = "XBLA Arcade (Internet Archive)" end
        if platform == "dlc"     then title = "DLC (Internet Archive - Hdd1)" end
        if platform == "xblig"   then title = "Xbox Indie Games (Internet Archive)" end
        if platform == "games"   then title = "Xbox 360 Games Archive (Internet Archive)" end
        if platform == "local"   then title = "Local Library (Transfer Folder)" end

        while true do
            collectgarbage()
            local r = Script.ShowPopupList(title, "Select Folder", bucketKeys)
            if not r or r.Canceled then break end
            
            local selectedKey = bucketKeys[r.Selected.Key]
            if not selectedKey or not buckets[selectedKey] then break end
            
            local gamesInBucket = buckets[selectedKey]
            table.sort(gamesInBucket)
            
            local g = Script.ShowPopupList(title .. " > " .. selectedKey, "Select Game", gamesInBucket)
            
            if g and not g.Canceled then
                local cleanName = gamesInBucket[g.Selected.Key]
                if not cleanName then break end
                
                local drives = {
                    "Hdd1:", "Usb0:", "Usb1:", "Usb2:", "Usb3:", "Usb4:",
                    "UsbMu0:", "UsbMu1:"
                }
                
                local dr = Script.ShowPopupList("Install to:", "", drives)
                
                if dr and not dr.Canceled then
                    gInstallDrive = drives[dr.Selected.Key]
                    
                    -- Ask for transfer mode
                    local transferModes = {
                        "HTTP (Download & Extract)",
                        "FTP (Direct Transfer - More Reliable)"
                    }
                    
                    local tm = Script.ShowPopupList("Transfer Method:", "Choose how to install", transferModes)
                    
                    if tm and not tm.Canceled then
                        if tm.Selected.Key == 1 then
                            gTransferMode = "http"
                        else
                            gTransferMode = "ftp"
                        end
                        
                        Script.SetStatus("Checking status...")
                        local state, msg = getGameStatus(cleanName)
                        local proceed = false

                        -- For FTP mode, always register with server first
                        if gTransferMode == "ftp" then
                            Script.SetStatus("Registering for FTP transfer...")
                            if not registerForFTP(cleanName, platform) then
                                -- Error already shown by registerForFTP
                                gTransferMode = "http"
                            end
                        end

                        -- Helper: show post-background hint appropriate to mode
                        local function showBackgroundedHint()
                            if gTransferMode == "ftp" then
                                Script.ShowMessageBox("Running in Background",
                                    "FTP transfer for '" .. cleanName .. "' continues on the server.\n\n" ..
                                    "Your game will appear in Aurora's content\n" ..
                                    "automatically when the transfer finishes.\n\n" ..
                                    "Use Server Queue & Status from the main menu\n" ..
                                    "to check progress.", "OK")
                            else
                                Script.ShowMessageBox("Running in Background",
                                    "'" .. cleanName .. "' is being prepared on the server.\n\n" ..
                                    "When it finishes, come back to this library,\n" ..
                                    "select the game again, and the install will\n" ..
                                    "start immediately (no re-download needed).\n\n" ..
                                    "Use Server Queue & Status to check progress.", "OK")
                            end
                        end

                        local waitResult = nil

                        if state == "Ready" and gTransferMode == "http" then
                            proceed = true
                        elseif state == "Ready" and gTransferMode == "ftp" then
                            if Script.ShowMessageBox("Transfer", "Game ready. Start FTP transfer to " .. gInstallDrive .. "?", "Yes", "No").Button == 1 then
                                if triggerDownload(cleanName, platform) then
                                    Script.SetStatus("Starting FTP transfer...")
                                    Thread.Sleep(2000)
                                    waitResult = waitForProcessing(cleanName)
                                end
                            end
                        elseif state == "Processing" then
                            waitResult = waitForProcessing(cleanName)
                        else
                            local modeText = "download"
                            if gTransferMode == "ftp" then
                                modeText = "process and FTP transfer"
                            end

                            if Script.ShowMessageBox("Download", "Start " .. modeText .. " for " .. cleanName .. "?", "Yes", "No").Button == 1 then
                                if triggerDownload(cleanName, platform) then
                                    Script.SetStatus("Starting...")
                                    Thread.Sleep(2000)
                                    waitResult = waitForProcessing(cleanName)
                                end
                            end
                        end

                        -- Resolve waitResult → proceed
                        if waitResult == true then
                            proceed = true
                        elseif waitResult == "backgrounded" then
                            showBackgroundedHint()
                            proceed = false
                        elseif waitResult == false then
                            proceed = false
                        end

                        if proceed then
                            -- Wrap entire install in pcall as final safety net
                            local installOk, installErr = pcall(installGame, cleanName)
                            if not installOk then
                                showError("INSTALL_FAILED",
                                    "Unexpected error during installation:\n" .. tostring(installErr))
                            end
                        end
                    end
                end
            end
        end
    else
        showError("SERVER_UNREACHABLE", err)
    end
end

function main()
    -- ── Step 1: Load saved IP from config (or keep the compiled-in default) ──
    local savedIP = loadConfig()
    if savedIP then
        BRAIN_IP = savedIP
    end
    initServerURL()   -- build SERVER_BASE / FILES_URL from BRAIN_IP

    -- ── Step 2: Network and server connectivity checks ──
    if not Aurora.HasInternetConnection() then
        showError("NO_NETWORK")
        return
    end

    local basePath = Script.GetBasePath()
    absoluteDownloadsPath = basePath .. DOWNLOAD_FOLDER .. "\\"

    local mkOk = pcall(FileSystem.CreateDirectory, absoluteDownloadsPath)
    if not mkOk then
        Script.ShowMessageBox("Error",
            "Could not create Downloads folder.\n" ..
            "Path: " .. absoluteDownloadsPath .. "\n\n" ..
            "The script storage may be read-only or full.", "OK")
        return
    end

    -- Quick server connectivity test on startup
    if not testServerConnection() then
        return
    end

    while true do
        Menu.ResetMenu()
        Menu.SetTitle("GODSend Store v6.3")

        -- Status / Queue
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Server Queue & Status  (Active Tasks)", {action = "SHOW_QUEUE"}))
        -- Local
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Local Library  (Your Transfer Folder ISOs)", {action = "BROWSE_LOCAL"}))
        -- Redump disc ISOs
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox 360 Redump ISOs  (Internet Archive)", {action = "BROWSE_360"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Original Xbox Redump ISOs  (Internet Archive)", {action = "BROWSE_OG"}))
        -- Arcade / Digital / DLC / Indie
        Menu.AddMainMenuItem(Menu.MakeMenuItem("XBLA Arcade  (Internet Archive)", {action = "BROWSE_XBLA"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Digital / No-Intro Titles  (Internet Archive)", {action = "BROWSE_DIGI"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("DLC Packages  (Internet Archive - Hdd1)", {action = "BROWSE_DLC"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox Live Indie Games  (Internet Archive)", {action = "BROWSE_XBLIG"}))
        -- General game archives (may be ISO, XEX, GOD)
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox 360 Game Archives  (Internet Archive)", {action = "BROWSE_GAMES"}))

        local ret, menu, canceled = Menu.ShowMainMenu()
        if canceled or not ret then break end

        if     ret.action == "SHOW_QUEUE"    then showQueue()
        elseif ret.action == "BROWSE_LOCAL"  then browseLibrary("local")
        elseif ret.action == "BROWSE_360"    then browseLibrary("xbox360")
        elseif ret.action == "BROWSE_OG"     then browseLibrary("xbox")
        elseif ret.action == "BROWSE_XBLA"   then browseLibrary("xbla")
        elseif ret.action == "BROWSE_DIGI"   then browseLibrary("digital")
        elseif ret.action == "BROWSE_DLC"    then browseLibrary("dlc")
        elseif ret.action == "BROWSE_XBLIG"  then browseLibrary("xblig")
        elseif ret.action == "BROWSE_GAMES"  then browseLibrary("games")
        end
    end
end
