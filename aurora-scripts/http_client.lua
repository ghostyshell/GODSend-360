-- ==============================
-- GODSend HTTP CLIENT
-- ==============================
-- Low-level HTTP helpers, utility formatters, error catalogue, and the
-- HTTP progress callback required by Http.GetEx.
-- Depends on: state.lua (BRAIN_IP, PORT, SERVER_BASE)

-- ── Error catalogue ───────────────────────────────────────────────────────────
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
        message = "Cannot reach the GODSend server.\n\n" ..
            "Troubleshooting:\n" ..
            "1. Verify the server (godsend.exe) is running on your PC\n" ..
            "2. If the IP is wrong, edit godsend_config.ini via FTP\n" ..
            "   and restart the script  (set ip=x.x.x.x under [Config])\n" ..
            "3. Make sure your PC and Xbox are on the same network\n" ..
            "4. Check your PC firewall allows the configured backend port\n" ..
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
            "1. Make sure your GODSend server version matches this script (v7.0)\n" ..
            "2. Restart the server application\n" ..
            "3. Check the server console for error messages"
    }
}

-- Show an error dialog with troubleshooting guidance.
function showError(errorKey, extraInfo)
    local err = ErrorHelp[errorKey]
    if not err then
        Script.ShowMessageBox("Error", extraInfo or "An unknown error occurred.", "OK")
        return
    end

    local msg = err.message

    -- Append the live server address for connectivity errors.
    if errorKey == "SERVER_UNREACHABLE" then
        msg = msg .. "\n\nTried: " .. (SERVER_BASE ~= "" and SERVER_BASE or BRAIN_IP .. ":" .. PORT)
    end

    if extraInfo and extraInfo ~= "" then
        msg = msg .. "\n\nDetails: " .. tostring(extraInfo)
    end

    Script.ShowMessageBox(err.title, msg, "OK")
end

-- Lua-owned copy: Aurora may reuse host buffers; sub(1,#s) forces a distinct string.
local function copyLuaString(s)
    if not s or type(s) ~= "string" or s == "" then return s end
    return string.sub(s, 1, #s)
end

-- Byte-wise rebuild so native UI (e.g. ShowPopupList rows) does not show text from a buffer
-- later overwritten when the next Http.Get URL is prepared (same class of bug as httpGet copy).
local function detachHostString(s)
    if type(s) ~= "string" or s == "" then return s end
    local n = #s
    local parts = {}
    for i = 1, n do
        parts[i] = string.char(string.byte(s, i))
    end
    return table.concat(parts)
end

-- Redump titles often end with ")." before ".iso". Aurora sometimes appends another ".",
-- producing ".." at the end — collapse those. Do not strip a single trailing "." (legitimate).
local function collapseDuplicateTrailingDots(s)
    while #s >= 2 and s:sub(-2, -1) == ".." do
        s = s:sub(1, -2)
    end
    local ff = "\239\188\142" -- U+FF0E fullwidth full stop (UTF-8)
    while #s >= 3 and s:sub(-3, -1) == ff do
        s = s:sub(1, -4)
    end
    return s
end

-- IniFile.ReadValue / downloaded manifest lines: Aurora often appends NUL tails or
-- control bytes so paths and URLs get junk suffixes (wrong filenames on disk).
function sanitizeManifestValue(s)
    if not s or type(s) ~= "string" then return "" end
    s = string.sub(s, 1, #s)
    if s:sub(1, 3) == "\239\187\191" then s = s:sub(4) end -- UTF-8 BOM
    local z = s:find("\0", 1, true)
    if z then s = s:sub(1, z - 1) end
    s = s:gsub("%c", "")
    local m = s:match("^%s*(.-)%s*$")
    return m or s
end

-- titlename field: same as manifest plus duplicate-trailing-dot collapse (browse titles).
function sanitizeIniTitleName(s)
    s = sanitizeManifestValue(s)
    if s == "" then return s end
    return collapseDuplicateTrailingDots(s)
end

-- Strip NUL padding and C0 control characters Aurora sometimes leaves on Http.Get
-- bodies and ShowPopupList return values (causes local ISO name mismatches on PC).
-- Also strip a leaked browse URL tail (host buffer reuse: e.g. "...USA)228:8080/browse?platform=local").
function sanitizeGameNameFromHost(s)
    if not s or type(s) ~= "string" then return "" end
    s = copyLuaString(s)
    -- Strip leaked GODsend browse URL (host buffer reuse). Align with Go browseURLLeakPattern:
    -- optional scheme, dotted host or short host:port, then /browse?platform=…
    s = s:gsub("https?://[%d%.]+:%d+/browse%?platform=[%w_]+", "")
    s = s:gsub("%d+:%d+/browse%?platform=[%w_]+", "")
    local z = s:find("\0", 1, true)
    if z then s = s:sub(1, z - 1) end
    s = s:gsub("%c", "")
    local m = s:match("^%s*(.-)%s*$")
    s = m or s
    s = collapseDuplicateTrailingDots(s)
    -- Letter-jump / quick-search: Aurora can append one ASCII letter after ")", e.g. "Open Season (USA)q"
    for _ = 1, 8 do
        local t = s:gsub("%)([a-zA-Z])$", ")")
        if t == s then break end
        s = t
    end
    -- Some Aurora UI buffers can append short prompt tails after ")" (e.g. "...Disc)in"
    -- or "...Disc)our PC"). Trim those so outgoing `game=` stays a clean title.
    for _ = 1, 8 do
        local trimmed, n = s:gsub("%)([A-Za-z ][A-Za-z ]*)$", ")")
        if n == 0 then break end
        local suffix = s:match("%)([A-Za-z ][A-Za-z ]*)$")
        if not suffix then break end
        suffix = suffix:match("^%s*(.-)%s*$") or suffix
        if #suffix == 0 or #suffix > 16 then
            break
        end
        s = trimmed
    end
    return detachHostString(s)
end

-- Truncate browse/HTTP text at first NUL (oversized Aurora response buffers).
function sanitizeBrowseBody(s)
    if not s or type(s) ~= "string" then return s end
    s = copyLuaString(s)
    local z = s:find("\0", 1, true)
    if z then return s:sub(1, z - 1) end
    return s
end

-- Encode a game title for the `game=` query parameter. Go parses queries as
-- application/x-www-form-urlencoded, where '+' is a space. If Http.UrlEncode
-- leaves literal '+' in the output and the name has no spaces, re-encode '+' as %2B
-- so filenames like "A+B" still match on the server.
function encodeGameQueryParam(name)
    if not name or name == "" then return nil end
    name = sanitizeGameNameFromHost(name)
    if name == "" then return nil end
    local e = Http.UrlEncode(name)
    if not e then return nil end
    if not name:find(" ", 1, true) and e:find("+", 1, true) then
        e = e:gsub("%+", "%%2B")
    end
    return e
end

-- ── Time / formatting helpers (global so services.lua can call getTime) ──────
function getTime()
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

local function formatDuration(secs)
    if not secs or secs < 0 then secs = 0 end
    secs = math.floor(secs)
    if secs < 60 then return secs .. "s" end
    return math.floor(secs / 60) .. "m" .. string.format("%02d", secs % 60) .. "s"
end

-- ── Public helpers (used by services.lua / menu.lua) ─────────────────────────

function httpGet(url)
    local ok, r = pcall(Http.Get, url)
    if not ok then return nil, "HTTP request threw an error" end
    if r and r.Success then
        local out = r.OutputData
        if type(out) == "string" and #out > 0 then
            -- Force a Lua-owned copy: Aurora may reuse host buffers so strings alias
            -- the next URL/response (game titles can pick up "228:8080/browse?platform=...").
            out = string.sub(out, 1, #out)
        end
        return out, nil
    end
    if r and r.StatusCode then
        return nil, "HTTP " .. tostring(r.StatusCode)
    end
    return nil, "No response from server"
end

function sanitizeForUrl(name)
    if not name then return "" end
    return name:gsub('[<>:"/\\|%?%*]', " -")
end

-- Extract a JSON string value for "field" with escape handling (\", \\, \n, \uXXXX).
-- The naive [^"]* pattern breaks when the message contains a quote or \ escapes.
function jsonField(json, field)
    if not json or type(json) ~= "string" then return nil end
    local ok, result = pcall(function()
        local prefix = '"' .. field .. '"%s*:%s*"'
        local _, e = json:find(prefix)
        if not e then return nil end
        local i = e + 1
        local parts = {}
        local n = #json
        while i <= n do
            local c = json:sub(i, i)
            if c == '"' then
                break
            end
            if c == "\\" and i < n then
                local esc = json:sub(i + 1, i + 1)
                if esc == '"' or esc == "\\" or esc == "/" then
                    table.insert(parts, esc)
                    i = i + 2
                elseif esc == "n" then
                    table.insert(parts, "\n")
                    i = i + 2
                elseif esc == "r" then
                    table.insert(parts, "\r")
                    i = i + 2
                elseif esc == "t" then
                    table.insert(parts, "\t")
                    i = i + 2
                elseif esc == "u" and i + 5 <= n then
                    local hex = json:sub(i + 2, i + 5)
                    local code = tonumber(hex, 16)
                    if code and code >= 32 and code <= 126 then
                        table.insert(parts, string.char(code))
                    elseif code and (code == 10 or code == 13) then
                        -- line breaks from JSON; host line is flattened in SetStatus anyway
                    elseif code then
                        table.insert(parts, "?")
                    end
                    i = i + 6
                else
                    table.insert(parts, esc)
                    i = i + 2
                end
            else
                table.insert(parts, c)
                i = i + 1
            end
        end
        return table.concat(parts)
    end)
    if ok then return result end
    return nil
end

-- Validate server response looks like valid JSON/text (not an HTML error page).
function validateResponse(data)
    if not data then return false end
    if type(data) ~= "string" then return false end
    if data:len() == 0 then return false end
    if data:sub(1, 1) == "<" and data:find("<html") then return false end
    return true
end

-- Get the Xbox's IP address for FTP registration.
function getXboxIP()
    local ok, ip = pcall(Aurora.GetIPAddress)
    if ok and ip and ip ~= "" then return ip end
    return "0.0.0.0"
end

-- ── HTTP Progress Callback ────────────────────────────────────────────────────
-- Must be global — Http.GetEx looks it up by name at call time.
function HttpProgressRoutine(dwTotalFileSize, dwTotalBytesTransferred, dwReason)
    local ok, result = pcall(function()
        if Script.IsCanceled() then
            gAbortedOperation = true
            return 1
        end

        local totalSize  = dwTotalFileSize or 0
        local transferred = dwTotalBytesTransferred or 0

        Script.SetProgress(transferred, totalSize)

        local now = getTime()
        if now > gLastProgressUpdate then
            local elapsed = now - gDownloadStartTime
            if elapsed < 1 then elapsed = 1 end

            local percent = 0
            if totalSize > 0 then
                percent = math.floor((transferred / totalSize) * 100)
                if percent > 100 then percent = 100 end
                if percent < 0  then percent = 0  end
            end

            local speedBytes = transferred / elapsed
            local speedStr   = formatSize(speedBytes) .. "/s"
            local downloadedStr = formatSize(transferred)
            local elapsedStr = formatDuration(elapsed)

            local etaStr = nil
            if totalSize > 0 and speedBytes > 0 and percent < 100 then
                local remaining = (totalSize - transferred) / speedBytes
                etaStr = "~" .. formatDuration(remaining) .. " left"
            end

            -- Build a single-line status string, ordered by importance:
            -- ETA (if known) → percent → part info/files → remaining details.
            local parts = {}

            if etaStr then
                table.insert(parts, "ETA " .. etaStr)
            end

            table.insert(parts, string.format("%d%%", percent))

            if gTotalParts and gTotalParts > 1 and gCurrentPart and gCurrentPart > 0 then
                table.insert(parts, string.format("Part %d/%d", gCurrentPart, gTotalParts))
            end

            table.insert(parts, downloadedStr)
            table.insert(parts, speedStr)
            table.insert(parts, "elapsed " .. elapsedStr)

            Script.SetStatus(table.concat(parts, " | "))
            gLastProgressUpdate = now
        end
        return 0
    end)

    if not ok then
        gAbortedOperation = true
        return 1
    end
    return result or 0
end
