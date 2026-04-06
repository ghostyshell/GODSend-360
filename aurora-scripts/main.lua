scriptTitle       = "GODSend Store"
scriptAuthor      = "Nesquin/david12549 & ghosty99"
scriptVersion     = "8.2.3"
scriptDescription = "Browse and install Xbox 360, Original, Digital (XBLA/DLC), and Retro ROMs via EdgeEmu!"
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
    -- Step 1: Load saved IP from config (or keep the compiled-in default).
    local savedIP = loadConfig()
    if savedIP then
        BRAIN_IP = savedIP
    end
    initServerURL()   -- build SERVER_BASE / FILES_URL from BRAIN_IP

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

    -- Step 3: Main menu loop.
    while true do
        Menu.ResetMenu()
        Menu.SetTitle("GODSend Store v8.2.3")

        Menu.AddMainMenuItem(Menu.MakeMenuItem("Server Queue & Status  (Active Tasks)",            {action = "SHOW_QUEUE"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Local Library  (Your Transfer Folder ISOs)",       {action = "BROWSE_LOCAL"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox 360 Redump ISOs  (Internet Archive)",         {action = "BROWSE_360"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Original Xbox Redump ISOs  (Internet Archive)",    {action = "BROWSE_OG"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("XBLA Arcade  (Internet Archive)",                  {action = "BROWSE_XBLA"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Digital / No-Intro Titles  (Internet Archive)",    {action = "BROWSE_DIGI"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("DLC Packages  (Internet Archive - Hdd1)",          {action = "BROWSE_DLC"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox Live Indie Games  (Internet Archive)",        {action = "BROWSE_XBLIG"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Xbox 360 Game Archives  (Internet Archive)",       {action = "BROWSE_GAMES"}))
        Menu.AddMainMenuItem(Menu.MakeMenuItem("Retro ROMs  (EdgeEmu - 62 Systems)",               {action = "BROWSE_ROMS"}))

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
