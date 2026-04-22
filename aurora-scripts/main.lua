scriptTitle       = "GODsend 360"
scriptAuthor      = "Nesquin/david12549 & ghosty99"
scriptVersion     = "2.11.0"
scriptDescription = "Browse and install Xbox 360, Original, Digital (XBLA/DLC), and Retro ROMs via Minerva Archive, Internet Archive, or EdgeEmu!"
scriptIcon        = "icon\\icon.xur"
scriptPermissions = { "http", "filesystem" }

require("menu_system")
require("state")
require("http_client")
require("services")
require("menu")

-- ==============================
-- ENTRY POINT
-- ==============================

function main()
    -- Step 1: Build SERVER_BASE / FILES_URL from state.lua values.
    -- Electron FTP deployment now patches BRAIN_IP and PORT directly in state.lua.
    initServerURL()

    -- Step 2: Network and server connectivity checks.
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

    if not testServerConnection() then
        return
    end

    loadServerConfig()

    -- Step 3: Main menu loop.
    while true do
        Menu.ResetMenu()
        Menu.SetTitle(scriptTitle .. " v" .. scriptVersion)

        Menu.AddMainMenuItem(Menu.MakeMenuItem("Server Queue & Status",              {action = "SHOW_QUEUE"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Local Library  (Transfer Folder)",   {action = "BROWSE_LOCAL"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox 360 Redump ISOs",              {action = "BROWSE_360"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Original Xbox Redump ISOs",         {action = "BROWSE_OG"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("XBLA Arcade",                       {action = "BROWSE_XBLA"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Digital / No-Intro Titles",         {action = "BROWSE_DIGI"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("DLC / Multi-Disc",                  {action = "BROWSE_DLC"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox Live Indie Games",             {action = "BROWSE_XBLIG"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox 360 Game Archives",            {action = "BROWSE_GAMES"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Retro ROMs  (62 Systems)",          {action = "BROWSE_ROMS"}))

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
        elseif ret.action == "BROWSE_ROMS"   then browseROMs()
        end
    end
end
