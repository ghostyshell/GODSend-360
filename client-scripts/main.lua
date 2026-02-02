scriptTitle = "GODSend Store"
scriptAuthor = "Nesquin/david12549"
scriptVersion = "6.0"
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
    local t = Aurora.GetTime()
    if t then return (t.Hour or 0) * 3600 + (t.Minute or 0) * 60 + (t.Second or 0) end
    return 0
end

local function formatSize(bytes)
    if bytes >= 1073741824 then
        return string.format("%.2f GB", bytes / 1073741824)
    elseif bytes >= 1048576 then
        return string.format("%.2f MB", bytes / 1048576)
    else
        return string.format("%.2f KB", bytes / 1024)
    end
end

local function httpGet(url)
    local r = Http.Get(url)
    if r and r.Success then return r.OutputData end
    return nil
end

local function sanitizeForUrl(name)
    return name:gsub('[<>:"/\\|%?%*]', " -")
end

-- Get the Xbox's IP address for FTP registration
local function getXboxIP()
    local ip = Aurora.GetIPAddress()
    if ip then
        return ip
    end
    return "0.0.0.0"
end

-- ==============================
-- HTTP PROGRESS CALLBACK
-- ==============================

function HttpProgressRoutine(dwTotalFileSize, dwTotalBytesTransferred, dwReason)
    if Script.IsCanceled() then
        gAbortedOperation = true
        return 1
    end
    Script.SetProgress(dwTotalBytesTransferred, dwTotalFileSize)

    local now = getTime()
    -- Update text every second to prevent flickering
    if now > gLastProgressUpdate then
        local elapsed = now - gDownloadStartTime
        if elapsed < 1 then elapsed = 1 end
        
        local percent = 0
        if dwTotalFileSize > 0 then
            percent = math.floor((dwTotalBytesTransferred / dwTotalFileSize) * 100)
        end

        local speedBytes = dwTotalBytesTransferred / elapsed
        local speedStr = formatSize(speedBytes) .. "/s"
        local downloadedStr = formatSize(dwTotalBytesTransferred)

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
end

-- ==============================
-- SERVER COMMUNICATION
-- ==============================

local function getGameStatus(gameName)
    local url = SERVER_BASE .. "/status?game=" .. Http.UrlEncode(gameName)
    local json = httpGet(url)
    if json then
        local state = json:match('"state"%s*:%s*"([^"]+)"')
        local message = json:match('"message"%s*:%s*"([^"]+)"')
        return state, message
    end
    return "Error", "No Response"
end

local function triggerDownload(gameName, platform)
    local url = SERVER_BASE .. "/trigger?game=" .. Http.UrlEncode(gameName) .. "&platform=" .. platform
    local json = httpGet(url)
    if json and (json:find("triggered") or json:find("already_ready")) then
        return true
    end
    Script.ShowMessageBox("Error", "Host did not confirm trigger.", "OK")
    return false
end

-- Register Xbox for FTP transfer with server
local function registerForFTP(gameName, platform)
    local xboxIP = getXboxIP()
    local url = SERVER_BASE .. "/register?game=" .. Http.UrlEncode(gameName) 
        .. "&ip=" .. Http.UrlEncode(xboxIP)
        .. "&drive=" .. Http.UrlEncode(gInstallDrive)
        .. "&platform=" .. platform
        .. "&mode=" .. gTransferMode
    
    local json = httpGet(url)
    if json and json:find("registered") then
        return true
    end
    return false
end

-- ==============================
-- WAIT FOR PROCESSING (SIMPLIFIED)
-- ==============================
-- Removed dead lockfile check - only polls /status endpoint

local function waitForProcessing(gameName)
    Script.ShowNotification("Initializing...")
    Thread.Sleep(2000)
    
    local dotCount = 0
    
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
            -- For FTP mode, "Ready" means transfer is complete
            if gTransferMode == "ftp" then
                Script.ShowNotification("FTP Transfer Complete!")
            else
                Script.ShowNotification("Download Ready!")
            end
            return true
        elseif state == "Processing" then
            Script.SetStatus("Host: " .. (message or "Processing") .. dots)
            Script.SetProgress(-1)
            dotCount = dotCount + 1
        elseif state == "Error" then
            Script.ShowMessageBox("Error", message or "Processing failed", "OK")
            return false
        else
            -- "Missing" or unknown state
            Script.SetStatus("Waiting for Host" .. dots)
            Script.SetProgress(-1)
            dotCount = dotCount + 1
        end
        
        -- Poll every 2 seconds (reduced from 1 second)
        Thread.Sleep(2000)
    end
end

-- ==============================
-- EXTRACTION LOGIC
-- ==============================

local function extractZipNative(zipPath, destFolder)
    local basePath = Script.GetBasePath()
    local relativePath = zipPath:gsub("^" .. basePath:gsub("\\", "\\\\"), "")

    local zip = ZipFile.OpenFile(relativePath)
    if not zip then return false end

    local tempExtract = DOWNLOAD_FOLDER .. "\\TempExtract"
    local tempAbs = basePath .. tempExtract

    if zip.Extract(zip, tempExtract .. "\\") then
        local moved = FileSystem.MoveDirectory(tempAbs .. "\\", destFolder, true)
        FileSystem.DeleteDirectory(tempAbs)
        return moved
    end
    return false
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
    -- For FTP mode, the server handles everything - just wait for completion
    if gTransferMode == "ftp" then
        Script.SetStatus("Server is transferring via FTP...")
        Script.SetProgress(-1)
        -- The waitForProcessing already happened, so just show success
        Script.ShowNotification("Installation Complete!")
        Script.SetRefreshListOnExit(true)
        return
    end
    
    -- HTTP MODE: Original download logic
    Script.SetStatus("Fetching Manifest...")

    local safeName = sanitizeForUrl(gameName)
    local gameBaseURL = FILES_URL .. Http.UrlEncode(safeName) .. "/"
    
    local iniUrl = gameBaseURL .. "godsend.ini"
    local localIniRel = DOWNLOAD_FOLDER .. "\\godsend.ini"
    local res = Http.GetEx(iniUrl, function(a,b,c) return 0 end, localIniRel)

    if not res then
        Script.ShowMessageBox("Error", "Failed to download Index from " .. iniUrl, "OK")
        return
    end

    local ini = IniFile.LoadFile(localIniRel)
    local installType = "god"
    if ini then installType = ini:ReadValue(gameName, "type", "god") end

    -- === PATH 1: DIGITAL (RAW) INSTALL ===
    if installType == "raw" then
        local rawFile = ini:ReadValue(gameName, "filename", ""):gsub("%s+", "")
        local relPath = ini:ReadValue(gameName, "path", ""):gsub("%s+", "")
        
        if rawFile == "" or relPath == "" then
            Script.ShowMessageBox("Error", "Invalid Raw Manifest", "OK")
            return
        end
        
        local currentPath = gInstallDrive .. "\\"
        for folder in relPath:gmatch("[^\\]+") do
            currentPath = currentPath .. folder .. "\\"
            FileSystem.CreateDirectory(currentPath)
        end
        local fullInstallPath = gInstallDrive .. "\\" .. relPath
        
        local safeTempName = "temp_raw.bin"
        local tempRawRel = DOWNLOAD_FOLDER .. "\\" .. safeTempName
        local tempRawAbs = absoluteDownloadsPath .. safeTempName
        local destAbs = fullInstallPath .. rawFile 
        
        if FileSystem.FileExists(tempRawAbs) then FileSystem.DeleteFile(tempRawAbs) end
        
        -- Set Globals for Progress Routine
        gCurrentPart = 1
        gTotalParts = 1
        gDownloadStartTime = getTime()
        
        local downloadUrl = gameBaseURL .. rawFile
        Script.SetStatus("Downloading " .. rawFile)
        
        if Http.GetEx(downloadUrl, HttpProgressRoutine, tempRawRel) then
            Script.SetStatus("Finalizing...")
            Thread.Sleep(500)
            
            if not FileSystem.FileExists(tempRawAbs) then
                Script.ShowMessageBox("Error", "Source file missing after download!", "OK")
                return
            end

            if FileSystem.FileExists(destAbs) then FileSystem.DeleteFile(destAbs) end
            
            local success = false
            if FileSystem.Rename(tempRawAbs, destAbs) then
                success = true
            elseif FileSystem.CopyFile(tempRawAbs, destAbs, function() end) then
                success = true
                FileSystem.DeleteFile(tempRawAbs)
            end
            
            if success then
                Script.ShowNotification("Installation Complete!")
                Script.SetRefreshListOnExit(true)
            else
                Script.ShowMessageBox("Error", "Failed to Install File", "OK")
            end
        else
            Script.ShowMessageBox("Error", "Download Failed", "OK")
        end
        
        FileSystem.DeleteFile(Script.GetBasePath() .. localIniRel)
        return
    end

    -- === PATH 2: STANDARD (GOD) INSTALL ===
    local downloadQueue, titleID, mediaID, dlcs = parseManifest(localIniRel, gameName)
    if not downloadQueue or #downloadQueue == 0 then
        Script.ShowMessageBox("Error", "Game Manifest Empty", "OK")
        return
    end

    if not titleID or titleID == "" or not mediaID or mediaID == "" then
        Script.ShowMessageBox("Error", "Missing TitleID/MediaID in INI.", "OK")
        return
    end

    local installPath = gInstallDrive .. "\\Content\\0000000000000000\\" .. titleID .. "\\" .. mediaID .. "\\"
    FileSystem.CreateDirectory(installPath)
    
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
        
        local dlRes = Http.GetEx(fullUrl, HttpProgressRoutine, dlRel)

        if gAbortedOperation or not dlRes then
            Script.ShowMessageBox("Error", "Download Failed (part " .. i .. ")", "OK")
            return
        end

        Script.SetStatus("Installing Part " .. i .. "...")
        local ok = extractZipNative(dlRel, installPath)
        FileSystem.DeleteFile(dlAbs)
        collectgarbage()

        if not ok then
            Script.ShowMessageBox("Error", "Installation Failed (part " .. i .. ")", "OK")
            return
        end
    end
    
    -- === 3. DLC INSTALL ===
    if dlcs and #dlcs > 0 then
        local dlcPath = gInstallDrive .. "\\Content\\0000000000000000\\" .. titleID .. "\\00000002\\"
        FileSystem.CreateDirectory(dlcPath)
        
        gTotalParts = #dlcs
        
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
            local dlRes = Http.GetEx(dlcUrlFull, HttpProgressRoutine, dlcRel)
            
            if dlRes then
                Script.SetStatus("Installing DLC " .. i .. "...")
                extractZipNative(dlcRel, dlcPath)
                FileSystem.DeleteFile(dlcAbs)
            else
                Script.ShowMessageBox("Warning", "DLC " .. i .. " failed to download.", "OK")
            end
            collectgarbage()
        end
    end

    Script.ShowNotification("Installation Complete!")
    Script.SetRefreshListOnExit(true)
    FileSystem.DeleteFile(Script.GetBasePath() .. localIniRel)
end

-- ==============================
-- MENU LOGIC
-- ==============================

function browseLibrary(platform)
    Script.SetStatus("Loading Library...")
    local list_data = httpGet(SERVER_BASE .. "/browse?platform=" .. platform)
    collectgarbage()

    if list_data then
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
            local gamesInBucket = buckets[selectedKey]
            table.sort(gamesInBucket)
            
            local g = Script.ShowPopupList(title .. " > " .. selectedKey, "Select Game", gamesInBucket)
            
            if g and not g.Canceled then
                local cleanName = gamesInBucket[g.Selected.Key]
                
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
                        "FTP (Direct Transfer - May be faster)"
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
                                Script.ShowMessageBox("Warning", "Failed to register FTP, falling back to HTTP", "OK")
                                gTransferMode = "http"
                            end
                        end

                        if state == "Ready" and gTransferMode == "http" then
                            -- HTTP mode and game already processed - can download immediately
                            proceed = true
                        elseif state == "Ready" and gTransferMode == "ftp" then
                            -- FTP mode but game already processed - need to re-trigger for FTP transfer
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
                            installGame(cleanName)
                        end
                    end
                end
            end
        end
    else
        Script.ShowMessageBox("Error", "Host Unreachable", "OK")
    end
end

function main()
    if not Aurora.HasInternetConnection() then
        Script.ShowMessageBox("Error", "No Network", "OK")
        return
    end

    local basePath = Script.GetBasePath()
    absoluteDownloadsPath = basePath .. DOWNLOAD_FOLDER .. "\\"
    FileSystem.CreateDirectory(absoluteDownloadsPath)

    while true do
        Menu.ResetMenu()
        Menu.SetTitle("GODSend Store v6.0")
        
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
