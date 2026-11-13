# Crea el .zip portable de dConnector de UNA arquitectura (docs/12-EMPAQUETADO.md §4) y lo comprueba.
# Uso: scripts\portable.ps1 [-Arch x64|x86|arm64] [-Version a.b.c[-sufijo]] [-Exe <dConnector.exe>] [-ServerExe <dConnector-server.exe>] [-Scratch <carpeta>]
# Salida: dist\dConnector-<versión>-portable-<arch>.zip (una carpeta raíz con ese nombre; la carpeta se arma en una carpeta temporal).
# Se EJECUTA (--version y --selftest --require-server) solo si esta máquina corre esa arquitectura; arm64 se verifica estructuralmente.
# Todo se puede repetir: borra y recrea su salida. No deja procesos vivos.
param(
    [ValidateSet('x86', 'x64', 'arm64')][string]$Arch = 'x64',
    [string]$Version = '',
    [string]$Exe = '',
    [string]$ServerExe = '',
    [string]$Scratch = ''
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'lib.ps1')

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $root
$v = Get-VersionInfo $Version $root
$version = $v.Full
$info = Get-ArchInfo $Arch
if (-not $Exe) { $Exe = "bin\windows-$Arch\dConnector.exe" }
if (-not $ServerExe) { $ServerExe = "bin\server\windows-$($info.GoArch)\dConnector-server.exe" }
$exePath = (Resolve-Path (Join-Path $root $Exe)).Path
$serverPath = (Resolve-Path (Join-Path $root $ServerExe)).Path

$name = "dConnector-$version-portable-$Arch"
$dist = Join-Path $root 'dist'
$zip = Join-Path $dist "$name.zip"
if (-not $Scratch) { $Scratch = Join-Path ([System.IO.Path]::GetTempPath()) ('dc-portable-' + [guid]::NewGuid().ToString('N')) }
$folder = Join-Path $Scratch "stage\$name"
New-Item -ItemType Directory -Force $dist | Out-Null
if (Test-Path $zip) { Remove-Item -Force $zip }
if (Test-Path $Scratch) { Remove-Item -Recurse -Force $Scratch }
New-Item -ItemType Directory -Force $folder | Out-Null

Write-Host "== Portable $Arch · $version =="
Assert-Machine $exePath $info
Assert-Machine $serverPath $info

# Archivos del paquete. portable.txt es solo marca (contenido literal, ASCII); LEEME y LICENCIAS ya van en UTF-8 con BOM.
Copy-Item $exePath (Join-Path $folder 'dConnector.exe')
Copy-Item $serverPath (Join-Path $folder 'dConnector-server.exe')
[System.IO.File]::WriteAllText((Join-Path $folder 'portable.txt'), 'Esta carpeta guarda los datos en .\data', (New-Object System.Text.UTF8Encoding($false)))
foreach ($f in 'LEEME.txt', 'LICENCIAS.txt') {
    $src = Join-Path $root $f
    if (-not (Test-Path $src)) { throw "falta $f en la raíz del proyecto" }
    Copy-Item $src (Join-Path $folder $f)
}
foreach ($d in (Get-OptionalDocs $root)) { Copy-Item $d (Join-Path $folder (Split-Path $d -Leaf)) }
$expected = @(Get-ChildItem $folder -File | ForEach-Object Name | Sort-Object)

# Zip con UNA carpeta raíz. Compress-Archive de Windows PowerShell 5.1 guarda las rutas con «\» (el formato ZIP pide «/»),
# así que el zip se escribe con System.IO.Compression y los nombres de entrada se construyen con «/». Orden y marcas de
# tiempo fijos (reproducible).
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$stamp = [DateTimeOffset]::new(2026, 1, 1, 0, 0, 0, [TimeSpan]::Zero)
$za = [System.IO.Compression.ZipFile]::Open($zip, [System.IO.Compression.ZipArchiveMode]::Create)
try {
    foreach ($file in (Get-ChildItem -Path $folder -Recurse -File | Sort-Object { $_.FullName.Substring($folder.Length) })) {
        $rel = $file.FullName.Substring($folder.Length).TrimStart('\', '/') -replace '\\', '/'
        $entry = $za.CreateEntry("$name/$rel", [System.IO.Compression.CompressionLevel]::Optimal)
        $entry.LastWriteTime = $stamp
        $es = $entry.Open()
        try { $bytes = [System.IO.File]::ReadAllBytes($file.FullName); $es.Write($bytes, 0, $bytes.Length) } finally { $es.Dispose() }
    }
} finally { $za.Dispose() }
Write-Host "Creado: $zip ($([math]::Round((Get-Item $zip).Length / 1MB, 1)) MB)"

# --- Comprobación automática ---
$test = Join-Path $Scratch 'portable-test'
New-Item -ItemType Directory -Force $test | Out-Null
$za = [System.IO.Compression.ZipFile]::OpenRead($zip)
try {
    $roots = @($za.Entries | ForEach-Object { $_.FullName.Split('/')[0] } | Sort-Object -Unique)
    $bad = @($za.Entries | Where-Object { $_.FullName.Contains('\') })
} finally { $za.Dispose() }
Assert-True (@($roots).Count -eq 1 -and $roots[0] -eq $name) "el zip tiene una sola carpeta raíz '$name'"
Assert-True ($bad.Count -eq 0) 'las rutas del zip usan «/»'

Expand-Archive -Path $zip -DestinationPath $test -Force
$pdir = Join-Path $test $name
$got = @(Get-ChildItem $pdir -File | ForEach-Object Name | Sort-Object)
Assert-True (($got -join '|') -eq ($expected -join '|')) "el zip contiene: $($got -join ', ')"
foreach ($f in 'dConnector.exe', 'dConnector-server.exe', 'portable.txt', 'LEEME.txt', 'LICENCIAS.txt') {
    if (-not (Test-Path (Join-Path $pdir $f))) { throw "El zip no contiene $f" }
}
Assert-Machine (Join-Path $pdir 'dConnector.exe') $info
Assert-Machine (Join-Path $pdir 'dConnector-server.exe') $info

if (-not (Test-CanRun $Arch)) {
    Write-Host "Solo verificación estructural: esta máquina no ejecuta $Arch (sin --version ni --selftest)."
    Remove-Item -Recurse -Force $Scratch -ErrorAction SilentlyContinue
    Write-Host "portable OK (estructural): $zip"
    return
}

# Sin DCONNECTOR_DATA: debe usar portable.txt -> <carpeta>\data.
Remove-Item Env:\DCONNECTOR_DATA -ErrorAction SilentlyContinue
$text = Test-Package $pdir $version $pdir @{}
Assert-True ($text -match 'portable: true') 'el selftest usó el modo portable (portable: true)'
$data = Join-Path $pdir 'data'
Assert-True (Test-Path $data) "existe la carpeta data junto al exe (modo portable: $data)"

Stop-StrayProcesses $Scratch
Remove-Item -Recurse -Force $Scratch -ErrorAction SilentlyContinue
Write-Host "portable OK: $zip"
