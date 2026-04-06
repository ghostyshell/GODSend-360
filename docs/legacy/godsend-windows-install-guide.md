# GODSend Homelab Edition — Windows Installation Guide

---

## What is GODSend?

GODSend is a tool for converting Xbox 360 ISO files to Games on Demand (GOD) format. This allows you to play your backup games directly from your Xbox 360’s hard drive or USB storage without needing the original disc.

---

## Why Use the Releases?

> **Important:** We strongly recommend using the official release installers from GitGud. These automated scripts will:

- Download all required files automatically
- Set up the correct directory structure for you
- Include all necessary tools (7-Zip utilities, iso2god converter)
- Create the files you need to transfer to your Xbox 360
- Save you time and prevent setup errors

---

## Prerequisites

Before you begin, make sure you have:

- Windows 10 or Windows 11 (64-bit recommended)
- At least 500MB of free disk space for the program
- Additional space for game downloads and conversions (15–25GB recommended for temporary storage)
- Stable internet connection for downloading games from Myrient
- A modded/JTAG/RGH Xbox 360 to use the converted files
- Aurora Dashboard installed on your Xbox 360 (recommended)

---

## Installation Steps

### Step 1: Download the Installer

1. Go to the official GODSend releases page:
   - **GitGud Releases:** [https://gitgud.io/Nesquin/godsend-homelab-edition/-/releases](https://gitgud.io/Nesquin/godsend-homelab-edition/-/releases)
2. Download the latest Windows installer:
   - Look for the Windows installer executable (`.exe` file)
   - Download from the latest release for the most recent features

### Step 2: Choose Installation Location

1. Create a folder where you want GODSend installed:
   - **Example:** `E:\godsend` or `C:\godsend`
   - Avoid spaces in the path (use `C:\godsend` not `C:\my games\godsend`)
   - Make sure you have write permissions to this folder
2. Run the installer and select your chosen directory:
   - The installer will download and extract all files to this location

### Step 3: Let the Installer Run

The installer will automatically:

- Download the latest GODSend files from GitHub
- Extract all necessary tools (7-Zip utilities, iso2god converter)
- Create the proper folder structure
- Set up the Xbox transfer files

**What to expect:**

- The installer may show download progress
- Installation typically takes 1–3 minutes depending on your internet speed
- You may see extraction/decompression activity

### Step 4: Verify Installation

After installation completes, your GODSend folder should contain:

```text
godsend/
│
├── 7za.dll                    # 7-Zip compression library
├── 7za.exe                    # 7-Zip command-line tool
├── 7zxa.dll                   # 7-Zip extraction library
├── godsend.exe                # Main conversion program
├── iso2god.exe                # ISO to GOD converter
│
├── MOVE_THESE_FILES_TO_XBOX/  # Files to transfer to your Xbox
│   ├── GODSend.ini            # Configuration file for Xbox
│   ├── main.lua               # Main Lua script
│   ├── MenuSystem.lua         # Menu system script
│   └── Icon/                  # Xbox dashboard icons
│       ├── frames.png
│       ├── icon.xui
│       └── icon.xur
│
├── Ready/                     # Converted GOD files go here
└── Temp/                      # Temporary files during conversion
```

**Check that you have:**

- [ ] All `.exe` and `.dll` files in the root directory
- [ ] `MOVE_THESE_FILES_TO_XBOX` folder with files inside
- [ ] Empty `Ready` and `Temp` folders (these will be used during conversion)

---

## How to Use GODSend

### Automatic Game Download from Myrient

Good news! GODSend automatically downloads Xbox 360 ISOs from Myrient, so you don’t need to manually source or download games yourself. The tool handles this for you!

### Converting Games

1. Run `godsend.exe` from your installation directory
2. Select the game you want to convert from the Myrient catalog:
   - GODSend will present you with available games
   - Choose your desired title
3. Wait for download and conversion:
   - GODSend downloads the ISO from Myrient to the `Temp` folder
   - Automatically extracts the ISO using 7-Zip
   - Converts it to GOD format using iso2god
   - Places the finished files in the `Ready` folder
   - Cleans up the `Temp` folder when done
4. Find your converted game in the `Ready` folder

### Using Your Own ISOs (Optional)

If you already have Xbox 360 ISO files downloaded:

1. **Check the naming convention used by Myrient:** ISOs from Myrient follow a specific naming format; your ISO files must match this exact naming scheme.
2. **Place your ISOs in the `Temp` folder:**
   - Navigate to your GODSend installation directory
   - Put your properly-named ISO files in the `Temp` folder
   - **Example path:** `E:\godsend\Temp\`
3. **Ensure correct naming:** Your ISO filenames must match Myrient’s naming convention exactly. Incorrect names will cause the conversion to fail. Check Myrient’s catalog for the proper naming format.
4. Run the conversion through GODSend as normal

---

## Setting Up the Xbox Connection

> **IMPORTANT:** Before transferring files to your Xbox, you need to configure the server connection.

1. **Find your computer’s IP address:**
   - Open Command Prompt (**Win + R**, type `cmd`, press Enter)
   - Type `ipconfig` and press Enter
   - Look for **“IPv4 Address”** under your active network connection
   - **Example:** `192.168.1.100`
2. **Edit `main.lua` in the `MOVE_THESE_FILES_TO_XBOX` folder:**
   - Open `main.lua` with a text editor (Notepad, Notepad++, VS Code, etc.)
   - Go to **line 13**
   - Change the IP address to your computer’s IPv4 address
   - **Example:** Change `192.168.1.1` to `192.168.1.100` (your actual IP)
   - Save the file

---

## Transferring Files to Xbox 360

You can transfer files using either FTP or Aurora File Manager. Choose the method you’re most comfortable with:

### Method 1: Using FTP (Recommended)

1. **Enable FTP on your Xbox 360** (if not already enabled):
   - In Aurora Dashboard, go to **Settings**
   - Enable **FTP server**
   - Note the FTP address shown (should match your Xbox IP)
2. **Connect using an FTP client** (FileZilla, WinSCP, or Windows File Explorer):
   - **Host:** Your Xbox 360’s IP address
   - **Port:** `21` (default)
   - Anonymous login (no username/password usually needed)
3. **Navigate to the Aurora scripts directory:**  
   `Hdd1:\Aurora\User\Scripts\Utility\`
4. **Create a new folder called `godsend`:**
   - Right-click → Create Directory → Name it `godsend`
5. **Transfer all files from `MOVE_THESE_FILES_TO_XBOX` into the `godsend` folder:**
   - `GODSend.ini`
   - `main.lua` (the one you edited with your IP address)
   - `MenuSystem.lua`
   - `Icon` folder (with all its contents)

**Final path should be:**

```text
Hdd1:\Aurora\User\Scripts\Utility\godsend\
├── GODSend.ini
├── main.lua
├── MenuSystem.lua
└── Icon\
    ├── frames.png
    ├── icon.xui
    └── icon.xur
```

### Method 2: Using Aurora File Manager

1. On your Xbox 360, launch Aurora Dashboard
2. Open **Aurora File Manager:**  
   Navigate to **Settings → File Manager** (or use the file browser)
3. Navigate to the scripts directory:  
   `Hdd1:\Aurora\User\Scripts\Utility\`
4. Create the `godsend` folder using the file manager
5. **Transfer files from USB:**
   - Copy the edited `MOVE_THESE_FILES_TO_XBOX` folder contents to a USB drive
   - Insert USB into Xbox 360
   - Use Aurora File Manager to copy files from USB to  
     `Hdd1:\Aurora\User\Scripts\Utility\godsend\`

---

## Transferring Converted Games

After converting your ISO files to GOD format:

1. **Copy your converted GOD files from the `Ready` folder to your Xbox:**
   - Transfer to your Xbox’s hard drive or USB storage
   - Place in the Games on Demand directory: `Hdd1:\Content\0000000000000000\`  
     **OR** use Aurora’s **“Scan Content”** feature to detect games automatically
2. **Refresh Aurora’s game library:**
   - In Aurora, go to **Settings → Content → Scan for Content**
   - Your converted games should now appear
3. Launch through your dashboard and enjoy disc-free gaming!

---

## Troubleshooting

### Download from Myrient Fails

- Check your internet connection
- Verify the Myrient server is accessible
- Ensure you have enough disk space in the `Temp` folder
- Try a different game if one specific title fails
- Check firewall settings aren’t blocking the download

### Using Own ISOs — Naming Issues

- Verify your ISO filename matches Myrient’s exact naming convention
- Check for extra spaces, wrong capitalization, or missing information
- Browse Myrient’s catalog to find the correct naming format
- Rename your ISO to match exactly before placing in `Temp` folder

### Can’t Connect to Xbox from GODSend

- Verify you edited `main.lua` with the correct IP address (line 13)
- Make sure your computer and Xbox are on the same network
- Check that your firewall isn’t blocking the connection
- Verify the Xbox’s FTP server is running
- Try pinging your Xbox from Command Prompt: `ping [xbox-ip]`

### Wrong IP Address in `main.lua`

- Find your correct IP using `ipconfig` in Command Prompt
- Re-edit `main.lua` line 13 with the correct IPv4 address
- Re-transfer the corrected `main.lua` to your Xbox
- Restart the GODSend script in Aurora

### FTP Connection Failed

- Ensure FTP is enabled in Aurora settings
- Check your Xbox’s IP address in **Aurora → Settings → Network**
- Use the correct port (usually 21)
- Some routers may block FTP — try connecting from the same subnet
- Try using a different FTP client (FileZilla, WinSCP)

### Files Not Showing in Aurora Scripts Menu

- Verify files are in the exact path: `Hdd1:\Aurora\User\Scripts\Utility\godsend\`
- Check that all files were transferred (`GODSend.ini`, `main.lua`, `MenuSystem.lua`, `Icon` folder)
- Restart Aurora Dashboard
- Check that the `godsend` folder name is spelled correctly (lowercase)

### Installer Won’t Run

- Make sure Windows Defender/antivirus isn’t blocking the file
- Verify you downloaded from the official GitGud release page
- Try running from a folder without special characters or spaces in the path

### Missing Files After Installation

- Re-run the installer
- Check your internet connection was stable during installation
- Make sure you have write permissions to the installation directory

### Conversion Fails

- Ensure the download completed successfully (check `Temp` folder)
- Verify you have enough disk space (2–3× the ISO size)
- If using your own ISO, ensure filename matches Myrient naming convention
- Check that all necessary `.dll` files are present in the GODSend directory
- Try a different game to rule out corrupt download

### `GODSend.exe` Won’t Launch

- Make sure all `.dll` files are present in the same directory
- Try running as administrator
- Check that your antivirus isn’t blocking it
- Ensure you have the latest Windows updates

### “Ready” Folder is Empty After Conversion

- Check the `Temp` folder for any stuck files
- Look for error messages in the console window
- Verify your ISO file is valid
- Ensure you have write permissions to the `Ready` folder

---

## Important Notes

- Always download from official sources (GitGud releases page)
- GODSend requires a modded Xbox 360 (JTAG/RGH) to use the converted files
- Ensure stable internet for downloading from Myrient
- The Windows installer is community-maintained by volunteer contributors
- Games are downloaded from Myrient — a game preservation archive

---

## Performance Tips

- Use a fast drive (SSD) for faster conversions
- Close other programs during conversion for better performance
- Keep your ISOs organized in a dedicated folder
- Clean the `Temp` folder periodically if conversions fail mid-process

---

## Getting Help

If you encounter issues:

1. Check your file structure matches the expected layout shown above
2. Verify all files are present (especially the `.dll` and `.exe` files)
3. Review console messages for specific error information
4. Visit the official repository at [https://gitgud.io/Nesquin/godsend-homelab-edition](https://gitgud.io/Nesquin/godsend-homelab-edition)
5. Submit an issue on GitGud with:
   - Your Windows version
   - Installation directory path
   - Error messages or screenshots
   - ISO file size and game title (if conversion-related)

---

## FAQ

**Q: Do I need to download Xbox 360 games myself?**  
**A:** No! GODSend automatically downloads games from the Myrient preservation archive. Just select the game you want and GODSend handles the download and conversion.

**Q: What is Myrient?**  
**A:** Myrient is a game preservation project that archives video game ISOs. GODSend integrates with Myrient to provide easy access to Xbox 360 game backups.

**Q: Can I use my own ISO files instead?**  
**A:** Yes, but they must be named exactly as Myrient names them. Place correctly-named ISOs in the `Temp` folder before running conversion.

**Q: How do I find the correct naming convention?**  
**A:** Browse the Myrient catalog or check the filenames of games GODSend downloads. Your ISOs must match this format exactly.

**Q: Why do I need to edit `main.lua` with my IP address?**  
**A:** GODSend runs a server on your Windows PC that the Xbox connects to for transferring converted games. The script needs to know your computer’s IP address to establish this connection.

**Q: Will my IP address change?**  
**A:** If you’re using DHCP (automatic IP), your IP might change when you restart your router or computer. Consider setting a static IP for your PC or updating `main.lua` if your IP changes.

**Q: Can I use the Xbox’s IP instead?**  
**A:** No, line 13 in `main.lua` needs your **computer’s** IP address (where GODSend is running), not the Xbox’s IP.

**Q: What if my computer and Xbox are on different networks?**  
**A:** They must be on the same local network for GODSend to work. Connect both to the same router/network.

**Q: Do I need administrator rights?**  
**A:** Not necessarily for running GODSend itself, but you may need them to run the installer or if your installation directory requires elevated permissions.

**Q: Can I move the installation folder after setup?**  
**A:** Yes, the entire `godsend` folder is portable. Just move it to your desired location.

**Q: How long does conversion take?**  
**A:** Depends on game size and your PC speed. Typically 2–10 minutes per game.

**Q: What’s the difference between ISO and GOD format?**  
**A:** ISO is a disc image. GOD (Games on Demand) is Microsoft’s format for storing games directly on Xbox storage without needing the disc.

**Q: Will this work on Xbox One or Series X/S?**  
**A:** No, GODSend is specifically for Xbox 360 games on modded Xbox 360 consoles.

---

## Next Steps

After successful installation:

1. Edit `main.lua` with your computer’s IP address
2. Transfer the configured files to your Xbox 360
3. Launch GODSend and browse the Myrient catalog
4. Select and download your first game
5. Transfer the converted GOD file to your Xbox
6. Set up Aurora to recognize GOD files
7. Enjoy disc-free gaming with your entire library!

---

## Links

| Resource | URL |
|----------|-----|
| Main Repository | [https://gitgud.io/Nesquin/godsend-homelab-edition](https://gitgud.io/Nesquin/godsend-homelab-edition) |
| Windows Installer (GitHub) | [https://github.com/my573ry/GODSendEXE/releases](https://github.com/my573ry/GODSendEXE/releases) |
| Official Releases | [https://gitgud.io/Nesquin/godsend-homelab-edition/-/releases](https://gitgud.io/Nesquin/godsend-homelab-edition/-/releases) |

---

## Additional Information

**About GOD format:** Games on Demand (GOD) format allows Xbox 360 games to be installed and played directly from the console’s hard drive or USB storage. This eliminates disc wear, reduces noise, and allows faster load times.

**Xbox 360 dashboard compatibility:** The files in `MOVE_THESE_FILES_TO_XBOX` are compatible with popular Xbox 360 dashboards like:

- Aurora Dashboard
- FreeStyle Dash (FSD)
- XEX Menu
- Other Lua-compatible dashboards

**Note for advanced users:** While Linux, Docker, and macOS are also supported platforms, this guide focuses on Windows installation as it’s the most common platform for users new to Xbox 360 modding and game conversion. If you’re comfortable with the command line on other platforms, you can set up the file structure manually.

The Windows installer was created by volunteer contributors to make GODSend more accessible to Windows users. Special thanks to **my573ry** and all contributors who made this easier for the community!

**Legal disclaimer:** This tool integrates with the Myrient game preservation archive for educational and preservation purposes. GODSend is intended for use with games you own or for preservation efforts. Always respect intellectual property rights and use this software in accordance with your local laws. The developers of GODSend are not responsible for how users choose to use this tool.

---

*This document is preserved as legacy reference alongside [legacy-installers-and-layout.md](legacy-installers-and-layout.md) for the GODsend-360 project.*
