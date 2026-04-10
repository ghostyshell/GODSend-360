-- ==============================
-- GODSend MENU
-- ==============================
-- Queue viewer and library browser (browse → drive → transfer mode → install).
-- Depends on: state.lua, http_client.lua, services.lua

-- ── Queue viewer ──────────────────────────────────────────────────────────────

-- Parse a flat JSON array returned by /queue.
local function parseQueueJSON(json)
    local entries = {}
    if not json or json == "" or json == "[]" then return entries end

    local pos = 1
    while true do
        local objStart = json:find("{", pos, true)
        if not objStart then break end

        local game  = json:match('"game"%s*:%s*"([^"]*)"',    objStart)
        local state = json:match('"state"%s*:%s*"([^"]*)"',   objStart)
        local msg   = json:match('"message"%s*:%s*"([^"]*)"', objStart)

        if game and game ~= "" and state and state ~= "" then
            table.insert(entries, { game = game, state = state, message = msg or "" })
        end

        local objEnd = json:find("}", objStart, true)
        pos = objEnd and (objEnd + 1) or (#json + 1)
    end
    return entries
end

-- State icon prefix for the popup list (ASCII only — no UTF-8).
local stateIcon = {
    Processing = " >> ",
    Ready      = " OK  ",
    Error      = " !! ",
    Missing    = "  ?  "
}

local function formatQueueEntry(e)
    local icon  = stateIcon[e.state] or "  ?  "
    local msg   = e.message or ""

    -- Try to parse a percentage from the message and show it first in the title.
    local pct = msg:match("%((%d+%.?%d*)%%%)") -- "(46.8%)" style
             or msg:match(":%s*(%d+)%%")        -- ": 75%"  style

    local title = e.game or "?"
    if pct then
        title = pct .. "% - " .. title
    end

    local label = "[" .. icon .. "] " .. title
    if msg ~= "" then
        local short = msg:sub(1, 35)
        if #msg > 35 then short = short .. "..." end  -- ASCII dots, not UTF-8 ellipsis
        label = label .. "  |  " .. short
    end
    return label
end

local function jobDetail(e)
    local game  = e.game    or "?"
    local state = e.state   or "?"
    local msg   = e.message or ""

    local pct = msg:match("%((%d+%.?%d*)%%%)") -- "(46.8%)" style
             or msg:match(":%s*(%d+)%%")        -- ": 75%"  style
    local bar = ""
    if pct then
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

        local data, err = httpGet(SERVER_BASE .. "/queue")
        if not data then
            Script.ShowMessageBox("Queue Error",
                "Could not reach the server.\n\n" .. tostring(err or "Unknown error"), "OK")
            return
        end

        local entries = parseQueueJSON(data)

        local cacheReady, cacheBuilding = 0, 0
        local cacheLines = {}
        local cacheOk, cacheData = pcall(httpGet, SERVER_BASE .. "/cache-status")
        if cacheOk and cacheData then
            for st in cacheData:gmatch('"state"%s*:%s*"([^"]*)"') do
                if st == "ready"    then cacheReady    = cacheReady    + 1 end
                if st == "building" then cacheBuilding = cacheBuilding + 1 end
            end
            for platform in cacheData:gmatch('"(%w+)"%s*:%s*%b{}') do
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
        if not sel then return end

        if sel == refreshRow then
            -- loop to refresh

        elseif sel == cacheRowIdx then
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
            -- Placeholder row — refresh

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
                        "Game: "  .. tostring(e.game)  ..
                        "\nState: " .. tostring(e.state) ..
                        "\nMsg: "   .. tostring(e.message), "OK")
                end
            elseif sub.Selected and sub.Selected.Key == 2 then
                local enc = encodeGameQueryParam(e.game)
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

-- ── ROM browser (EdgeEmu — 62 systems) ───────────────────────────────────────

-- Ordered system list matching romSystems in the Go backend.
local romSystemList = {
    {id = "a2600",      name = "Atari - 2600"},
    {id = "a5200",      name = "Atari - 5200"},
    {id = "a7800",      name = "Atari - 7800"},
    {id = "jaguar",     name = "Atari - Jaguar"},
    {id = "jaguarcd",   name = "Atari - Jaguar CD"},
    {id = "lynx",       name = "Atari - Lynx"},
    {id = "st",         name = "Atari - ST"},
    {id = "ws",         name = "Bandai - WonderSwan"},
    {id = "coleco",     name = "Coleco - ColecoVision"},
    {id = "c64",        name = "Commodore - 64"},
    {id = "amiga",      name = "Commodore - Amiga"},
    {id = "amigacd",    name = "Commodore - Amiga CD"},
    {id = "amigacd32",  name = "Commodore - Amiga CD32"},
    {id = "plus4",      name = "Commodore - Plus/4"},
    {id = "vic20",      name = "Commodore - VIC-20"},
    {id = "channelf",   name = "Fairchild - Channel F"},
    {id = "vectrex",    name = "GCE - Vectrex"},
    {id = "msx",        name = "Microsoft - MSX / MSX2"},
    {id = "pcecd",      name = "NEC - PC Engine CD"},
    {id = "sgx",        name = "NEC - PC Engine SuperGrafx"},
    {id = "pce",        name = "NEC - PC Engine / TurboGrafx 16"},
    {id = "nds",        name = "Nintendo - DS"},
    {id = "fds",        name = "Nintendo - Famicom Disk System"},
    {id = "gb",         name = "Nintendo - Game Boy"},
    {id = "gba",        name = "Nintendo - Game Boy Advance"},
    {id = "gbc",        name = "Nintendo - Game Boy Color"},
    {id = "gc",         name = "Nintendo - GameCube"},
    {id = "n64",        name = "Nintendo - 64"},
    {id = "nes",        name = "Nintendo - NES"},
    {id = "sat",        name = "Nintendo - Satellaview"},
    {id = "vb",         name = "Nintendo - Virtual Boy"},
    {id = "snes",       name = "Nintendo - SNES"},
    {id = "3do",        name = "Panasonic - 3DO"},
    {id = "cdi",        name = "Philips - CDi"},
    {id = "studioii",   name = "RCA - Studio II"},
    {id = "32x",        name = "Sega - 32X"},
    {id = "dc",         name = "Sega - Dreamcast"},
    {id = "gg",         name = "Sega - Game Gear"},
    {id = "sms",        name = "Sega - Master System / Mark III"},
    {id = "scd",        name = "Sega - Mega-CD / Sega CD"},
    {id = "genesis",    name = "Sega - Mega Drive / Genesis"},
    {id = "pico",       name = "Sega - PICO"},
    {id = "saturn",     name = "Sega - Saturn"},
    {id = "sg1000",     name = "Sega - SG-1000"},
    {id = "zx",         name = "Sinclair - ZX Spectrum +3"},
    {id = "ngcd",       name = "SNK - Neo Geo CD"},
    {id = "ngpc",       name = "SNK - Neo Geo Pocket Color"},
    {id = "supervision",name = "Watara - Supervision"},
}

-- Top-level system picker.
function browseROMs()
    while true do
        collectgarbage()
        local sysNames = {}
        for _, s in ipairs(romSystemList) do
            table.insert(sysNames, s.name)
        end

        local r = Script.ShowPopupList("Retro ROMs (EdgeEmu)", "Select System", sysNames)
        if not r or r.Canceled then break end

        local selected = romSystemList[r.Selected.Key]
        if selected then
            browseROMLibrary("rom_" .. selected.id, selected.name)
        end
    end
end

-- Game browser for one ROM system, delegates install to installGame (type=rom).
function browseROMLibrary(platform, sysName)
    Script.SetStatus("Loading " .. sysName .. "...")
    local list_data, err = httpGet(SERVER_BASE .. "/browse?platform=" .. platform)
    collectgarbage()

    if not list_data then
        showError("SERVER_UNREACHABLE", err)
        return
    end

    if list_data:sub(1, 14) == "__IA_LOADING__" then
        local loaded, total = list_data:match("__IA_LOADING__:(%d+)/(%d+)")
        local progressMsg = ""
        if loaded and total then
            progressMsg = string.format("\nProgress: %s / %s fetched.", loaded, total)
        end
        Script.ShowMessageBox("Library Loading",
            sysName .. " list is loading from EdgeEmu.\n" ..
            progressMsg .. "\n\nGo back and try again in a moment.\n" ..
            "Cached after the first load.", "OK")
        return
    end

    if not validateResponse(list_data) then
        showError("HTTP_PARSE_ERROR", "Browse returned invalid data")
        return
    end

    list_data = sanitizeBrowseBody(list_data)

    local buckets    = {}
    local bucketKeys = {}
    for game in list_data:gmatch("([^|]+)") do
        game = sanitizeGameNameFromHost(game)
        if game ~= "" then
            local firstChar = string.upper(string.sub(game, 1, 1))
            if string.match(firstChar, "%d") then firstChar = "#" end
            if not buckets[firstChar] then
                buckets[firstChar] = {}
                table.insert(bucketKeys, firstChar)
            end
            table.insert(buckets[firstChar], game)
        end
    end

    if #bucketKeys == 0 then
        Script.ShowMessageBox("Empty Library",
            "No ROMs found for " .. sysName .. ".\n\n" ..
            "The list may still be loading — try again shortly.", "OK")
        return
    end

    table.sort(bucketKeys, function(a, b)
        if a == "#" then return true end
        if b == "#" then return false end
        return a < b
    end)

    local title = sysName .. "  (EdgeEmu)"

    while true do
        collectgarbage()
        local r = Script.ShowPopupList(title, "Select Folder", bucketKeys)
        if not r or r.Canceled then break end

        local selectedKey = bucketKeys[r.Selected.Key]
        if not selectedKey or not buckets[selectedKey] then break end

        local gamesInBucket = buckets[selectedKey]
        table.sort(gamesInBucket)

        while true do
            collectgarbage()
            local g = Script.ShowPopupList(title .. " > " .. selectedKey,
                                           "Select Game", gamesInBucket)
            if not g or g.Canceled then break end

            local cleanName = sanitizeGameNameFromHost(gamesInBucket[g.Selected.Key])
            if not cleanName or cleanName == "" then break end

            -- Drive picker
            local drives = {
                "Hdd1:", "Usb0:", "Usb1:", "Usb2:", "Usb3:", "Usb4:",
                "UsbMu0:", "UsbMu1:"
            }
            local dr = Script.ShowPopupList("Install to:", "", drives)
            if not dr or dr.Canceled then
                -- back to game list
            else
                gInstallDrive = drives[dr.Selected.Key]

                local transferModes = {
                    "FTP (Direct Transfer - More Reliable)",
                    "HTTP (Download & Extract)"
                }
                local tm = Script.ShowPopupList("Transfer Method:", "Choose how to install", transferModes)
                if tm and not tm.Canceled then
                    gTransferMode = (tm.Selected.Key == 1) and "ftp" or "http"

                    Script.SetStatus("Checking status...")
                    local state, msg = getGameStatus(cleanName)

                    if gTransferMode == "ftp" then
                        Script.SetStatus("Registering for FTP...")
                        if not registerForFTP(cleanName, platform) then
                            gTransferMode = "http"
                        end
                    end

                    local waitResult = nil

                    if state == "Ready" and gTransferMode == "http" then
                        waitResult = true
                    elseif state == "Ready" and gTransferMode == "ftp" then
                        if Script.ShowMessageBox("Transfer",
                            "ROM ready. Start FTP to " .. gInstallDrive .. "?",
                            "Yes", "No").Button == 1 then
                            if triggerDownload(cleanName, platform) then
                                Thread.Sleep(2000)
                                waitResult = waitForProcessing(cleanName)
                            end
                        end
                    elseif state == "Processing" then
                        waitResult = waitForProcessing(cleanName)
                    else
                        local modeText = (gTransferMode == "ftp")
                            and "process and FTP transfer" or "download"
                        if Script.ShowMessageBox("Download",
                            "Start " .. modeText .. " for\n" .. cleanName .. "?",
                            "Yes", "No").Button == 1 then
                            if triggerDownload(cleanName, platform) then
                                Thread.Sleep(2000)
                                waitResult = waitForProcessing(cleanName)
                            end
                        end
                    end

                    if waitResult == true then
                        local ok, err2 = pcall(installGame, cleanName)
                        if not ok then
                            showError("INSTALL_FAILED",
                                "ROM install error:\n" .. tostring(err2))
                        end
                    elseif waitResult == "backgrounded" then
                        Script.ShowMessageBox("Running in Background",
                            "'" .. cleanName .. "' is being prepared.\n\n" ..
                            "Select it again when ready to install.", "OK")
                    end
                end
            end
        end -- game list loop
    end
end

-- ── Install method (GOD / DLC / XEX) ───────────────────────────────────────────

-- Optional [Recommended] from /disc-info (Transfer ISO probe or filename hint).
local function getDiscRecommendation(gameName)
    local enc = encodeGameQueryParam(gameName)
    if not enc then return nil end
    local json, _ = httpGet(SERVER_BASE .. "/disc-info?game=" .. enc)
    if not json then return nil end
    local rec = json:match('"recommendation"%s*:%s*"([^"]*)"')
    local notes = json:match('"notes"%s*:%s*"([^"]*)"')
    return rec, notes
end

-- GOD = ISO to GOD; DLC = content partition to Content\...\00000002\; XEX = loose default.xex folder.
-- Shown for every title on Xbox 360 / Original / Local / Games Archive libraries.
-- Returns "god", "content", "xex", or nil if cancelled.
local function pickInstallMethod(gameName, platform)
    if platform == "games" or platform == "xbox360" or platform == "xbox" or platform == "local" then
        local rec, notes = getDiscRecommendation(gameName)
        local g = "GOD  — ISO to Games on Demand"
        local d = "DLC  — Content folder (from ISO content tree)"
        local x = "XEX  — Loose folder (default.xex in archive)"
        if rec == "god" then
            g = g .. "  [Recommended]"
        elseif rec == "content" then
            d = d .. "  [Recommended]"
        end
        local sub = "How should the server package this title?"
        if notes and notes ~= "" then
            sub = sub .. "\n\n" .. notes
        end
        local r = Script.ShowPopupList("Install Method", sub, { g, d, x })
        if not r or r.Canceled then return nil end
        if r.Selected.Key == 1 then return "god" end
        if r.Selected.Key == 2 then return "content" end
        return "xex"
    end
    return "god"
end

-- ── Library browser ───────────────────────────────────────────────────────────

function browseLibrary(platform)
    -- For online platforms, pick source first so the list and trigger both use the same source.
    local browseSource = ""
    if platform ~= "local" then
        local sourceItems = {"Minerva Archive", "Internet Archive"}
        local sr = Script.ShowPopupList("Download Source:",
            "Choose which library to browse", sourceItems)
        if not sr or sr.Canceled then return end
        browseSource = (sr.Selected.Key == 1) and "minerva" or "ia"
    end

    local sourceLabel = (browseSource == "minerva") and "Minerva" or
                        (browseSource == "ia")      and "Internet Archive" or ""

    Script.SetStatus("Loading Library...")
    local browseURL = SERVER_BASE .. "/browse?platform=" .. platform
    if browseSource ~= "" then
        browseURL = browseURL .. "&source=" .. browseSource
    end
    local list_data, err = httpGet(browseURL)
    collectgarbage()

    if list_data then
        -- Handle cache loading marker (format: __IA_LOADING__:loaded/total)
        if list_data:sub(1, 14) == "__IA_LOADING__" then
            local loaded, total = list_data:match("__IA_LOADING__:(%d+)/(%d+)")
            local progressMsg = ""
            if loaded and total then
                progressMsg = string.format("\nProgress: %s / %s collections fetched.", loaded, total)
            end
            local srcName = sourceLabel ~= "" and sourceLabel or "the source"
            Script.ShowMessageBox("Library Loading",
                "The " .. srcName .. " game list is still loading.\n" ..
                progressMsg .. "\n\n" ..
                "Please go back and try again in a moment.\n" ..
                "After the first load, the list is saved to disk.", "OK")
            return
        end

        if not validateResponse(list_data) then
            showError("HTTP_PARSE_ERROR", "Browse returned invalid data")
            return
        end

        list_data = sanitizeBrowseBody(list_data)

        local buckets    = {}
        local bucketKeys = {}

        for game in list_data:gmatch("([^|]+)") do
            game = sanitizeGameNameFromHost(game)
            if game ~= "" then
                local firstChar = string.upper(string.sub(game, 1, 1))
                if string.match(firstChar, "%d") then firstChar = "#" end

                if not buckets[firstChar] then
                    buckets[firstChar] = {}
                    table.insert(bucketKeys, firstChar)
                end
                table.insert(buckets[firstChar], game)
            end
        end

        if #bucketKeys == 0 then
            local emptyMsg
            if platform == "local" then
                emptyMsg = "No ISO files found in the Transfer folder.\n\n" ..
                    "Place your .iso files in the Transfer folder on\n" ..
                    "your PC next to godsend.exe, then try again."
            else
                local srcName = sourceLabel ~= "" and sourceLabel or "this source"
                emptyMsg = "No games found in " .. srcName .. ".\n\n" ..
                    "The game list may still be loading.\n" ..
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

        local title = "Xbox 360 Redump"
        if platform == "xbox"    then title = "Original Xbox Redump" end
        if platform == "digital" then title = "Digital / No-Intro" end
        if platform == "xbla"    then title = "XBLA Arcade" end
        if platform == "dlc"     then title = "DLC / Multi-Disc" end
        if platform == "xblig"   then title = "Xbox Indie Games" end
        if platform == "games"   then title = "Xbox 360 Games Archive" end
        if platform == "local"   then title = "Local Library (Transfer Folder)" end

        while true do
            collectgarbage()
            local r = Script.ShowPopupList(title, "Select Folder", bucketKeys)
            if not r or r.Canceled then break end

            local selectedKey = bucketKeys[r.Selected.Key]
            if not selectedKey or not buckets[selectedKey] then break end

            local gamesInBucket = buckets[selectedKey]
            table.sort(gamesInBucket)

            while true do
                collectgarbage()
                local g = Script.ShowPopupList(title .. " > " .. selectedKey,
                                               "Select Game", gamesInBucket)

                if not g or g.Canceled then break end

                local cleanName = sanitizeGameNameFromHost(gamesInBucket[g.Selected.Key])
                if not cleanName or cleanName == "" then break end

                local driveReady = false
                local drives = {
                    "Hdd1:", "Usb0:", "Usb1:", "Usb2:", "Usb3:", "Usb4:",
                    "UsbMu0:", "UsbMu1:"
                }
                local dr = Script.ShowPopupList("Install to:", "", drives)
                if dr and not dr.Canceled then
                    gInstallDrive = drives[dr.Selected.Key]
                    driveReady = true
                end

                if driveReady then
                    local transferModes = {
                        "FTP (Direct Transfer - More Reliable)",
                        "HTTP (Download & Extract)"
                    }

                    local tm = Script.ShowPopupList("Transfer Method:",
                                                    "Choose how files reach the console", transferModes)

                    if tm and not tm.Canceled then
                        gTransferMode = (tm.Selected.Key == 1) and "ftp" or "http"

                        local method = pickInstallMethod(cleanName, platform)
                        if method then
                            gInstallType = method

                        Script.SetStatus("Checking status...")
                        local state, msg = getGameStatus(cleanName)
                        local proceed = false

                        if gTransferMode == "ftp" then
                            Script.SetStatus("Registering for FTP transfer...")
                            if not registerForFTP(cleanName, platform, gInstallType) then
                                gTransferMode = "http"
                            end
                        end

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
                            if Script.ShowMessageBox("Transfer",
                                "Game ready. Start FTP transfer to " .. gInstallDrive .. "?",
                                "Yes", "No").Button == 1 then
                                if triggerDownload(cleanName, platform, gInstallType, browseSource) then
                                    Script.SetStatus("Starting FTP transfer...")
                                    Thread.Sleep(2000)
                                    waitResult = waitForProcessing(cleanName)
                                end
                            end
                        elseif state == "Processing" then
                            waitResult = waitForProcessing(cleanName)
                        else
                            local modeText = (gTransferMode == "ftp")
                                and "process and FTP transfer"
                                or  "download"

                            if Script.ShowMessageBox("Download",
                                "Start " .. modeText .. " for " .. cleanName .. "?",
                                "Yes", "No").Button == 1 then
                                if triggerDownload(cleanName, platform, gInstallType, browseSource) then
                                    Script.SetStatus("Starting...")
                                    Thread.Sleep(2000)
                                    waitResult = waitForProcessing(cleanName)
                                end
                            end
                        end

                        if waitResult == true then
                            proceed = true
                        elseif waitResult == "backgrounded" then
                            showBackgroundedHint()
                            proceed = false
                        elseif waitResult == false then
                            proceed = false
                        end

                        if proceed then
                            local installOk, installErr = pcall(installGame, cleanName)
                            if not installOk then
                                showError("INSTALL_FAILED",
                                    "Unexpected error during installation:\n" .. tostring(installErr))
                            end
                        end
                        end -- if method (install type chosen)
                    end -- if transfer popup not canceled
                end -- if driveReady
            end -- game list loop
        end
    else
        showError("SERVER_UNREACHABLE", err)
    end
end
