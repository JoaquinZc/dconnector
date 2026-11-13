# Ejecuta `dConnector.exe --selftest` con los datos en una carpeta temporal y exige «OK».
# Uso: scripts\selftest.ps1 -Exe bin\dConnector.exe [-DataDir <carpeta>] [-Keep] [-RequireServer]
# -RequireServer: falla si no hay dConnector-server.exe junto al exe (así se comprueban los paquetes y `task gate`).
param(
    [Parameter(Mandatory = $true)][string]$Exe,
    [string]$DataDir = "",
    [switch]$Keep,
    [switch]$RequireServer
)
$ErrorActionPreference = 'Stop'

$exePath = (Resolve-Path $Exe).Path
$ownTemp = $false
if (-not $DataDir) {
    $DataDir = Join-Path ([System.IO.Path]::GetTempPath()) ("dc-selftest-" + [guid]::NewGuid().ToString('N'))
    $ownTemp = $true
}
New-Item -ItemType Directory -Force $DataDir | Out-Null
$outFile = Join-Path $DataDir 'selftest.txt'
$argList = @('--selftest')
if ($RequireServer) { $argList += '--require-server' }
$argList += @('--out', "`"$outFile`"")

$env:DCONNECTOR_DATA = $DataDir
try {
    $p = Start-Process -FilePath $exePath -ArgumentList $argList -Wait -PassThru -WindowStyle Hidden
} finally {
    Remove-Item Env:\DCONNECTOR_DATA -ErrorAction SilentlyContinue
}

$text = if (Test-Path $outFile) { Get-Content $outFile -Raw } else { '' }
Write-Host $text
if ($p.ExitCode -ne 0 -or $text -notmatch '(?m)^OK') {
    throw "selftest falló (código $($p.ExitCode))"
}
if ($ownTemp -and -not $Keep) { Remove-Item -Recurse -Force $DataDir -ErrorAction SilentlyContinue }
Write-Host 'selftest OK'
