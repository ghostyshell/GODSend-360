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
local BRAIN_IP = "192.168.1.100" -- YOUR PC IP HERE.
local PORT = "8080"
local SERVER_BASE = "http://" .. BRAIN_IP .. ":" .. PORT
local FILES_URL   = SERVER_BASE .. "/files/"
local DOWNLOAD_FOLDER = "Downloads"

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
        message = "Cannot reach GODSend server at " .. BRAIN_IP .. ":" .. PORT .. "\n\n" ..
            "Troubleshooting:\n" ..
            "1. Verify the server is running on your PC\n" ..
            "2. Check BRAIN_IP in script settings matches your PC's IP\n" ..
            "3. Make sure your PC and Xbox are on the same network\n" ..
            "4. Check your PC firewall allows port " .. PORT .. "\n" ..
            "5. If using Pi-hole/DNS filter, whitelist server IP"
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

            local status = ""
            if gTotalParts > 1 then
                status = string.format("Part %d/%d: %d%% | %s | %s", 
                    gCurrentPart, gTotalParts, percent, downloadedStr, speedStr)
            else
                status = string.format("Downloading: %d%% | %s | %s", 
                    percent, downloadedStr, speedStr)
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

local function waitForProcessing(gameName)
    Script.ShowNotification("Initializing...")
    Thread.Sleep(2000)
    
    local dotCount = 0
    local failCount = 0
    local maxFails = 15  -- 15 consecutive failures = 30 seconds of no response
    
    while true do
        -- Check for user cancellation
        if Script.IsCanceled() then 
            return false 
        end
        
        -- Memory management
        collectgarbage()
        
        -- Single HTTP request per loop iteration
        local state, message = getGameStatus(gameName)
        local dots = string.rep(".", dotCount % 4)
        
        if state == "Ready" then
            if gTransferMode == "ftp" then
                Script.ShowNotification("FTP Transfer Complete!")
            else
                Script.ShowNotification("Download Ready!")
            end
            failCount = 0
            return true
        elseif state == "Processing" then
            Script.SetStatus("Host: " .. (message or "Processing") .. dots)
            Script.SetProgress(-1)
            dotCount = dotCount + 1
            failCount = 0  -- Reset on successful status check
        elseif state == "Error" then
            local errorDetail = message or "Processing failed on server"
            showError("DOWNLOAD_FAILED", errorDetail)
            return false
        else
            -- "Missing" or communication failure
            failCount = failCount + 1
            
            if failCount >= maxFails then
                showError("DOWNLOAD_TIMEOUT", 
                    "Lost contact with server after " .. (failCount * 2) .. " seconds")
                return false
            end
            
            Script.SetStatus("Waiting for Host" .. dots .. " (" .. failCount .. "/" .. maxFails .. ")")
            Script.SetProgress(-1)
            dotCount = dotCount + 1
        end
        
        -- Poll every 2 seconds
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
    if not ini then return nil, nil, nil, nil end

    local titleID = ini:ReadValue(gameName, "titleid", "")
    local mediaID = ini:ReadValue(gameName, "mediaid", "")
    local parts = {}
    local dlcs = {}

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

    return parts, titleID, mediaID, dlcs
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

    -- === PATH 1: DIGITAL (RAW) INSTALL ===
    if installType == "raw" then
        local rawFile = ini:ReadValue(gameName, "filename", ""):gsub("%s+", "")
        local relPath = ini:ReadValue(gameName, "path", ""):gsub("%s+", "")
        
        if rawFile == "" or relPath == "" then
            showError("MANIFEST_EMPTY", "Raw manifest missing filename or path")
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end
        
        -- Create directory structure safely
        local currentPath = gInstallDrive .. "\\"
        for folder in relPath:gmatch("[^\\]+") do
            currentPath = currentPath .. folder .. "\\"
            local mkOk = pcall(FileSystem.CreateDirectory, currentPath)
            if not mkOk then
                showError("INSTALL_FAILED", "Could not create directory: " .. currentPath)
                pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
                return
            end
        end
        local fullInstallPath = gInstallDrive .. "\\" .. relPath
        
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
    local downloadQueue, titleID, mediaID, dlcs = parseManifest(localIniRel, gameName)
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

    local installPath = gInstallDrive .. "\\Content\\0000000000000000\\" .. titleID .. "\\" .. mediaID .. "\\"
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
    
    -- === 3. DLC INSTALL ===
    if dlcs and #dlcs > 0 then
        local dlcPath = gInstallDrive .. "\\Content\\0000000000000000\\" .. titleID .. "\\00000002\\"
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
-- MENU LOGIC
-- ==============================

function browseLibrary(platform)
    Script.SetStatus("Loading Library...")
    local list_data, err = httpGet(SERVER_BASE .. "/browse?platform=" .. platform)
    collectgarbage()

    if list_data then
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
            Script.ShowMessageBox("Empty Library", 
                "No games found in this library.\n\n" ..
                "This could mean:\n" ..
                "1. Myrient may be temporarily down\n" ..
                "2. Network issue between your server and Myrient\n" ..
                "3. Check the server console for connection errors", "OK")
            return
        end

        table.sort(bucketKeys, function(a, b)
            if a == "#" then return true end
            if b == "#" then return false end
            return a < b
        end)

        local title = "Xbox 360"
        if platform == "xbox" then title = "Original Xbox" end
        if platform == "digital" then title = "Digital Library" end

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

                        if state == "Ready" and gTransferMode == "http" then
                            proceed = true
                        elseif state == "Ready" and gTransferMode == "ftp" then
                            if Script.ShowMessageBox("Transfer", "Game ready. Start FTP transfer to " .. gInstallDrive .. "?", "Yes", "No").Button == 1 then
                                if triggerDownload(cleanName, platform) then
                                    Script.SetStatus("Starting FTP transfer...")
                                    Thread.Sleep(2000) 
                                    proceed = waitForProcessing(cleanName)
                                end
                            end
                        elseif state == "Processing" then
                            proceed = waitForProcessing(cleanName)
                        else
                            local modeText = "download"
                            if gTransferMode == "ftp" then
                                modeText = "process and FTP transfer"
                            end
                            
                            if Script.ShowMessageBox("Download", "Start " .. modeText .. " for " .. cleanName .. "?", "Yes", "No").Button == 1 then
                                if triggerDownload(cleanName, platform) then
                                    Script.SetStatus("Starting...")
                                    Thread.Sleep(2000) 
                                    proceed = waitForProcessing(cleanName)
                                end
                            end
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
        Menu.SetTitle("GODSend Store v6.1")
        
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox 360 Library", {action = "BROWSE_360"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Original Xbox Library", {action = "BROWSE_OG"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Digital Library (XBLA/DLC)", {action = "BROWSE_DIGI"}))

        local ret, menu, canceled = Menu.ShowMainMenu()
        if canceled or not ret then break end

        if ret.action == "BROWSE_360" then
            browseLibrary("xbox360")
        elseif ret.action == "BROWSE_OG" then
            browseLibrary("xbox")
        elseif ret.action == "BROWSE_DIGI" then
            browseLibrary("digital")
        end
    end
end
