# Uses npm.cmd so this works even when PowerShell blocks npm.ps1 (execution policy).
$env:CSC_IDENTITY_AUTO_DISCOVERY = "false"
Set-Location (Resolve-Path "$PSScriptRoot\..\..\")
& "$env:ProgramFiles\nodejs\npm.cmd" run build
exit $LASTEXITCODE
