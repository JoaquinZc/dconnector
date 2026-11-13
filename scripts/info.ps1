# Genera el info.json del recurso de Windows con la VERSION del repositorio (el de build/windows es la plantilla).
# Uso: scripts\info.ps1 [-Version a.b.c[-sufijo]] [-Out bin\obj\info.json]
param([string]$Version = '', [string]$Out = 'bin\obj\info.json')
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'lib.ps1')
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$v = Get-VersionInfo $Version $root
$j = Get-Content (Join-Path $root 'build\windows\info.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$j.fixed.file_version = $v.Num
$j.fixed.product_version = $v.Num
$j.info.'0409'.FileVersion = $v.Full
$j.info.'0409'.ProductVersion = $v.Full
$dest = if ([System.IO.Path]::IsPathRooted($Out)) { $Out } else { Join-Path $root $Out }
New-Item -ItemType Directory -Force (Split-Path $dest) | Out-Null
[System.IO.File]::WriteAllText($dest, ($j | ConvertTo-Json -Depth 5), (New-Object System.Text.UTF8Encoding($false)))
Write-Host "info.json -> $dest ($($v.Full))"
