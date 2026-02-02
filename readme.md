GODSend

GODSend is a network-based content management and installation bridge for Xbox 360 consoles running the Aurora dashboard. It consists of a Go-based backend (PC) and a Lua frontend (Xbox) to automate the processing, conversion, and transfer of backup files over a local network.
Features

    Automated Conversion: Automatically converts standard ISO disc backups into the Games on Demand (GOD) format required by the console.

    Direct Network Installation: Bypasses the need for USB drives by serving content directly via HTTP.

    Format Support: Handles standard Disc images, Digital/XBLA titles, and DLC packages.

    Smart File Handling:

        Includes a "Rigid" extraction protocol to ensure 100% file integrity during transfer.

        Automated filename sanitization to prevent syntax errors (Error 123) on the destination drive.

        Recursive directory creation for complex DLC/XBLA file structures.

    Storage Agnostic: Supports installation to HDD, USB, and UsbMu partitions.

Requirments:
7zip 19.00
Iso2GOD - rs

Expected folder structure:

->Ready
->Temp
->7z.OS executable extension
->godsend_(OS Name Here).OS executable extension
->iso2GOD.OS executable extension

