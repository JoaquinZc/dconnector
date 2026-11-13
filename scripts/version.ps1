# Imprime la versión de dConnector.exe y exige que coincida con el archivo VERSION.
# Uso: scripts\version.ps1 -Exe bin\dConnector.exe
param([Parameter(Mandatory = $true)][string]$Exe)
$ErrorActionPreference = 'Stop'

$exePath = (Resolve-Path $Exe).Path
$outFile = Join-Path ([System.IO.Path]::GetTempPath()) ("dc-version-" + [guid]::NewGuid().ToString('N') + '.txt')
$p = Start-Process -FilePath $exePath -ArgumentList '--version', '--out', "`"$outFile`"" -Wait -PassThru -WindowStyle Hidden
$got = if (Test-Path $outFile) { (Get-Content $outFile -Raw).Trim() } else { '' }
Remove-Item $outFile -ErrorAction SilentlyContinue

$want = (Get-Content (Join-Path $PSScriptRoot '..\VERSION') -Raw).Trim()
Write-Host "dConnector.exe --version -> '$got' (VERSION: '$want')"
if ($p.ExitCode -ne 0 -or $got -ne $want) { throw 'La versión del ejecutable no coincide con VERSION' }
