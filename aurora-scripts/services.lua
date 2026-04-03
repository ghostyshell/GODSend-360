-- ==============================
-- GODSend SERVICES
-- ==============================
-- Server communication, game processing, and installation logic.
-- Depends on: state.lua, http_client.lua

-- ── Server communication ──────────────────────────────────────────────────────

function getGameStatus(gameName)
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

    local state   = jsonField(json, "state")
    local message = jsonField(json, "message")

    if state then return state, message or "" end
    return "Error", "Could not parse server response"
end

function triggerDownload(gameName, platform)
    if not gameName or gameName == "" then
        showError("TRIGGER_FAILED", "No game name provided")
        return false
    end

    local encodedName = Http.UrlEncode(gameName)
    if not encodedName then
        showError("TRIGGER_FAILED", "Failed to encode game name")
        return false
    end

    local url = SERVER_BASE .. "/trigger?game=" .. encodedName
        .. "&platform=" .. (platform or "xbox360")
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

    -- Local-only mode: server does not use Internet Archive.
    if json:find("local_unavailable") then
        local msg = json:match('"message"%s*:%s*"([^"]*)"')
            or "No ISO found in the PC Transfer folder."
        Script.ShowMessageBox("Local Transfer", msg, "OK")
        return false
    end

    showError("TRIGGER_FAILED", "Server response: " .. json:sub(1, 100))
    return false
end

-- Register Xbox for FTP transfer with server.
function registerForFTP(gameName, platform)
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
        .. "&ip="       .. Http.UrlEncode(xboxIP)
        .. "&drive="    .. Http.UrlEncode(gInstallDrive)
        .. "&platform=" .. (platform or "xbox360")
        .. "&mode="     .. gTransferMode

    local json, err = httpGet(url)

    if not json then
        showError("FTP_REGISTER_FAILED", err)
        return false
    end

    if json:find("registered") then return true end

    showError("FTP_REGISTER_FAILED", "Unexpected server response")
    return false
end

function testServerConnection()
    Script.SetStatus("Testing server connection...")
    local json, err = httpGet(SERVER_BASE .. "/status?game=__ping__")
    if not json then
        showError("SERVER_UNREACHABLE", err)
        return false
    end
    return true
end

-- ── Processing wait loop ──────────────────────────────────────────────────────

-- waitForProcessing polls the server until the job is Ready or fails.
-- Returns:
--   true           — job finished, proceed to install
--   false          — error or user chose to abort
--   "backgrounded" — user dismissed the window; server keeps running
function waitForProcessing(gameName)
    Script.ShowNotification("Initializing...")
    Thread.Sleep(2000)

    local dotCount = 0
    local failCount = 0
    local maxFails = 15  -- 15 consecutive failures = ~30 seconds of no response

    while true do
        if Script.IsCanceled() then
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

            local choice = Script.ShowMessageBox(promptTitle, promptBody, "Background", "Abort")

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
            -- Aurora's progress UI does not show multi-line SetStatus text reliably;
            -- keep one line and collapse any embedded newlines from JSON.
            local line = msg:gsub("[\r\n]+", " "):gsub("  +", " ")
            Script.SetStatus("Host: " .. line .. dots)

            local pct = msg:match("%((%d+%.?%d*)%%%)") -- "(46.8%)" style
                     or msg:match(":%s*(%d+)%%")        -- ": 75%" style
            pct = tonumber(pct)
            if pct then
                Script.SetProgress(math.floor(pct), 100)
            else
                Script.SetProgress(-1)
            end

            dotCount  = dotCount + 1
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

-- ── Extraction ────────────────────────────────────────────────────────────────

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

-- ── Manifest parsing ──────────────────────────────────────────────────────────

local function parseManifest(iniPath, gameName)
    local ini = IniFile.LoadFile(iniPath)
    if not ini then return nil, nil, nil, nil, nil end

    local titleID   = ini:ReadValue(gameName, "titleid",   "")
    local mediaID   = ini:ReadValue(gameName, "mediaid",   "")
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

-- ── Game installation ─────────────────────────────────────────────────────────

function installGame(gameName)
    -- For FTP mode the server handles everything.
    if gTransferMode == "ftp" then
        Script.SetStatus("Server is transferring via FTP...")
        Script.SetProgress(-1)
        Script.ShowMessageBox("Installation Complete",
            "Game has been transferred via FTP.\n\n" ..
            "Go to Settings > Content > Scan to refresh\n" ..
            "your game library.", "OK")
        return
    end

    -- HTTP MODE: download logic with full error handling.
    Script.SetStatus("Fetching Manifest...")

    local safeName = sanitizeForUrl(gameName)
    if not safeName or safeName == "" then
        showError("MANIFEST_FAILED", "Game name could not be sanitized")
        return
    end

    local gameBaseURL = FILES_URL .. Http.UrlEncode(safeName) .. "/"

    local iniUrl     = gameBaseURL .. "godsend.ini"
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
        local xexPart    = ini:ReadValue(gameName, "dataurl", "")
            :gsub("%%20", " "):gsub("%%28", "("):gsub("%%29", ")")

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
        gTotalParts  = 1

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
        local rawFile   = ini:ReadValue(gameName, "filename", ""):gsub("%s+", "")
        local relPath   = ini:ReadValue(gameName, "path",     ""):gsub("%s+", "")
        local forcedDrive = ini:ReadValue(gameName, "drive",  ""):gsub("%s+", "")

        if rawFile == "" or relPath == "" then
            showError("MANIFEST_EMPTY", "Raw manifest missing filename or path")
            pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
            return
        end

        local dlcDrive = (forcedDrive ~= "") and forcedDrive or "Hdd1:"
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
        local tempRawRel   = DOWNLOAD_FOLDER .. "\\" .. safeTempName
        local tempRawAbs   = absoluteDownloadsPath .. safeTempName
        local destAbs      = fullInstallPath .. rawFile

        pcall(FileSystem.DeleteFile, tempRawAbs)

        gCurrentPart = 1
        gTotalParts  = 1
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

        local success   = false
        local moveError = ""

        local renameOk = pcall(function()
            if FileSystem.Rename(tempRawAbs, destAbs) then
                success = true
            end
        end)

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
    local downloadQueue, titleID, mediaID, dlcs, titleName =
        parseManifest(localIniRel, gameName)

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

    local godPrefix = (titleName and titleName ~= "") and titleName or "Title"
    local installPath = gInstallDrive .. "\\GOD\\" .. godPrefix .. " - " .. titleID
        .. "\\" .. mediaID .. "\\"
    local mkOk = pcall(FileSystem.CreateDirectory, installPath)
    if not mkOk then
        showError("INSTALL_FAILED", "Could not create install directory on " .. gInstallDrive)
        pcall(FileSystem.DeleteFile, Script.GetBasePath() .. localIniRel)
        return
    end

    gTotalParts = #downloadQueue

    for i, urlFrag in ipairs(downloadQueue) do
        local fullUrl  = gameBaseURL .. urlFrag
        local fileName = "part" .. i .. ".7z"
        local dlRel    = DOWNLOAD_FOLDER .. "\\" .. fileName
        local dlAbs    = absoluteDownloadsPath .. fileName

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

    -- DLC install (always to Hdd1).
    if dlcs and #dlcs > 0 then
        local dlcPath = "Hdd1:\\Content\\0000000000000000\\" .. titleID .. "\\00000002\\"
        pcall(FileSystem.CreateDirectory, dlcPath)

        gTotalParts = #dlcs
        local dlcFailures = {}

        for i, dlcUrlFrag in ipairs(dlcs) do
            local dlcUrlFull     = gameBaseURL .. dlcUrlFrag
            local dlcArchiveName = "dlc_temp_" .. i .. ".7z"
            local dlcRel         = DOWNLOAD_FOLDER .. "\\" .. dlcArchiveName
            local dlcAbs         = absoluteDownloadsPath .. dlcArchiveName

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
