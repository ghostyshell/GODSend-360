!macro preInit
  SetRegView 64
  StrCpy $INSTDIR "$PROGRAMFILES64\GODsend"
  WriteRegExpandStr HKLM "${INSTALL_REGISTRY_KEY}" InstallLocation "$INSTDIR"
  WriteRegExpandStr HKCU "${INSTALL_REGISTRY_KEY}" InstallLocation "$INSTDIR"
!macroend

!macro customInstall
  CreateDirectory "$INSTDIR\Temp"
  CreateDirectory "$INSTDIR\Transfer"
  CreateDirectory "$INSTDIR\Ready"
  ; Allow inbound HTTP (8080) for any process — covers godsend-backend.exe without a second firewall prompt.
  ExecWait 'cmd.exe /c netsh advfirewall firewall delete rule name="GODsend HTTP 8080" 2>nul'
  ExecWait 'cmd.exe /c netsh advfirewall firewall add rule name="GODsend HTTP 8080" dir=in action=allow protocol=TCP localport=8080 profile=any'
  ; Allow aria2c through the firewall for BitTorrent (inbound peers + outbound DHT/tracker).
  ExecWait 'cmd.exe /c netsh advfirewall firewall delete rule name="GODsend aria2c" 2>nul'
  ExecWait 'cmd.exe /c netsh advfirewall firewall add rule name="GODsend aria2c" dir=in  action=allow program="$INSTDIR\aria2c.exe" profile=any'
  ExecWait 'cmd.exe /c netsh advfirewall firewall add rule name="GODsend aria2c" dir=out action=allow program="$INSTDIR\aria2c.exe" profile=any'
  ${ifNot} ${isNoDesktopShortcut}
    ; Use the installed executable icon to avoid broken icon paths.
    CreateShortcut "$DESKTOP\GODsend.lnk" "$INSTDIR\GODsend.exe" "" "$INSTDIR\GODsend.exe" 0
  ${endIf}
!macroend

!macro customUnInstall
  ExecWait 'cmd.exe /c netsh advfirewall firewall delete rule name="GODsend HTTP 8080"'
  ExecWait 'cmd.exe /c netsh advfirewall firewall delete rule name="GODsend aria2c"'
!macroend
