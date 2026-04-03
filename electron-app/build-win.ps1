# Uses npm.cmd so this works even when PowerShell blocks npm.ps1 (execution policy).
$env:CSC_IDENTITY_AUTO_DISCOVERY = "false"
Set-Location $PSScriptRoot
& "$env:ProgramFiles\nodejs\npm.cmd" run build:win
exit $LASTEXITCODE
