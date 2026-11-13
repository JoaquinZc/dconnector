# Genera LICENCIAS.txt (el que viaja en el MSI y en el zip portable) y THIRD-PARTY-NOTICES (raíz del repositorio)
# a partir de los módulos reales que entran en los ejecutables (go list -deps de cmd/dconnector y de
# cmd/dconnector-server, en Windows y Linux) y de los paquetes de la interfaz
# (frontend\node_modules\<paquete>\package.json: hace falta haber corrido «pnpm install» en frontend).
# Los dos archivos llevan el mismo cuerpo y distinta cabecera.
# No forma parte de la compilación: se ejecuta a mano cuando cambian las dependencias.
# Uso: scripts\licencias.ps1 [-Out LICENCIAS.txt] [-Notices THIRD-PARTY-NOTICES]
param([string]$Out = '', [string]$Notices = '')
$ErrorActionPreference = 'Stop'

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (-not $Out) { $Out = Join-Path $root 'LICENCIAS.txt' }
if (-not $Notices) { $Notices = Join-Path $root 'THIRD-PARTY-NOTICES' }
$crlf = "`r`n"

# Módulos cuyo texto completo se incluye siempre (los demás van en la tabla; los de copyleft también llevan texto).
$conTexto = @(
    'go.mau.fi/whatsmeow', 'github.com/gotd/td', 'go.mau.fi/mautrix-meta', 'github.com/wailsapp/wails/v3',
    'modernc.org/sqlite', 'github.com/coder/websocket', 'github.com/oklog/ulid/v2', 'google.golang.org/protobuf',
    'golang.org/x/crypto', 'golang.org/x/image', 'golang.org/x/net', 'golang.org/x/sys', 'golang.org/x/text',
    'golang.org/x/sync', 'golang.org/x/exp', 'maunium.net/go/mautrix', 'go.uber.org/multierr', 'go.mau.fi/util',
    'github.com/rs/zerolog'
)
$enlaces = @{
    'go.mau.fi/whatsmeow'            = 'https://github.com/tulir/whatsmeow'
    'github.com/gotd/td'             = 'https://github.com/gotd/td'
    'go.mau.fi/mautrix-meta'         = 'https://github.com/mautrix/meta'
    'github.com/wailsapp/wails/v3'   = 'https://github.com/wailsapp/wails'
    'modernc.org/sqlite'             = 'https://gitlab.com/cznic/sqlite'
}

function Get-LicenseFile([string]$dir) {
    if (-not $dir -or -not (Test-Path $dir)) { return $null }
    foreach ($n in 'LICENSE', 'LICENSE.txt', 'LICENSE.md', 'LICENCE', 'LICENCE.txt', 'COPYING', 'COPYING.txt', 'UNLICENSE', 'LICENSE-MIT', 'LICENSE.MIT') {
        $f = Join-Path $dir $n
        if (Test-Path $f -PathType Leaf) { return $f }
    }
    $any = Get-ChildItem -Path $dir -File -ErrorAction SilentlyContinue | Where-Object { $_.Name -match '^(LICEN[CS]E|COPYING|UNLICENSE)' } | Select-Object -First 1
    if ($any) { return $any.FullName }
    return $null
}

function Get-Spdx([string]$text) {
    if (-not $text) { return 'sin archivo de licencia' }
    $head = $text.Substring(0, [Math]::Min(400, $text.Length))
    if ($head -match 'GNU AFFERO GENERAL PUBLIC LICENSE') { return 'AGPL-3.0' }
    if ($head -match 'GNU LESSER GENERAL PUBLIC LICENSE') { return 'LGPL' }
    if ($head -match 'GNU GENERAL PUBLIC LICENSE') {
        if ($head -match 'Version 3') { return 'GPL-3.0' }
        if ($head -match 'Version 2') { return 'GPL-2.0' }
        return 'GPL'
    }
    if ($text -match 'Mozilla Public License,? Version 2\.0') { return 'MPL-2.0' }
    if ($text -match 'Apache License' -and $text -match 'Version 2\.0') { return 'Apache-2.0' }
    if ($text -match 'Permission is hereby granted, free of charge') { return 'MIT' }
    if ($text -match 'free and unencumbered software released into the public domain') { return 'Unlicense' }
    if ($text -match 'Permission to use, copy, modify, and/or distribute this software for any\s+purpose with or without fee') { return 'ISC' }
    if ($text -match 'Permission to use, copy, modify, and distribute this software for any\s+purpose with or without fee') { return 'ISC' }
    if ($text -match 'Redistribution and use in source and binary forms') {
        if ($text -match 'Neither the name|endorse or promote') { return 'BSD-3-Clause' }
        return 'BSD-2-Clause'
    }
    if ($text -match 'MIT License') { return 'MIT' }
    return 'desconocida (revisar texto)'
}

function Read-Text([string]$f) {
    if (-not $f) { return '' }
    return ([System.IO.File]::ReadAllText($f, [System.Text.Encoding]::UTF8)).Trim() -replace "`r?`n", $crlf
}

# --- Módulos de Go ---
# Unión de lo que compila cada ejecutable y plataforma (el servidor en Linux puede traer módulos que Windows no).
$objetivos = @(
    @{ Os = 'windows'; Arch = 'amd64'; Pkg = './cmd/dconnector' },
    @{ Os = 'windows'; Arch = 'amd64'; Pkg = './cmd/dconnector-server' },
    @{ Os = 'linux';   Arch = 'amd64'; Pkg = './cmd/dconnector-server' },
    @{ Os = 'linux';   Arch = 'arm64'; Pkg = './cmd/dconnector-server' }
)
$env:CGO_ENABLED = '0'
$lines = @()
Push-Location $root
try {
    foreach ($t in $objetivos) {
        $env:GOOS = $t.Os; $env:GOARCH = $t.Arch
        $r = & go list -deps -tags production -f '{{if .Module}}{{if not .Standard}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}{{end}}' $t.Pkg 2>$null
        if ($LASTEXITCODE -ne 0 -or -not $r) { throw ("go list falló para {0} ({1}/{2})" -f $t.Pkg, $t.Os, $t.Arch) }
        $lines += $r
    }
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
    $goroot = (& go env GOROOT).Trim()
} finally { Pop-Location }

$mods = @{}
foreach ($l in ($lines | Sort-Object -Unique)) {
    $p = $l -split '\|', 3
    if ($p.Count -lt 3 -or $p[0] -eq 'dconnector' -or -not $p[1]) { continue }
    $mods[$p[0]] = [pscustomobject]@{ Path = $p[0]; Version = $p[1]; Dir = $p[2] }
}
# Módulos sin archivo de licencia propio: se declara la licencia de su origen (se lee de sus cabeceras).
$sinArchivo = @{
    'github.com/beeper/poly1305' = 'BSD-3-Clause'   # derivado de golang.org/x/crypto (cabecera «The Go Authors»); aplica el texto de Go de más abajo
}
$goMods = $mods.Values | Sort-Object Path
foreach ($m in $goMods) {
    $f = Get-LicenseFile $m.Dir
    $m | Add-Member -NotePropertyName File -NotePropertyValue $f
    $m | Add-Member -NotePropertyName Text -NotePropertyValue (Read-Text $f)
    $spdx = Get-Spdx $m.Text
    if (-not $f -and $sinArchivo.ContainsKey($m.Path)) { $spdx = $sinArchivo[$m.Path] + '*' }
    $m | Add-Member -NotePropertyName Spdx -NotePropertyValue $spdx
}

# --- Paquetes de la interfaz ---
$npmNames = 'react', 'react-dom', 'zustand', 'lucide-react', 'recharts', '@tanstack/react-virtual', 'date-fns',
            'emoji-picker-react', 'qrcode.react', '@wailsio/runtime'
$npm = foreach ($n in $npmNames) {
    $pj = Join-Path $root "frontend\node_modules\$n\package.json"
    if (-not (Test-Path $pj)) { Write-Warning "no está instalado: $n"; continue }
    $j = Get-Content $pj -Raw -Encoding UTF8 | ConvertFrom-Json
    $lic = if ($j.license) { [string]$j.license } elseif ($j.licenses) { ($j.licenses | ForEach-Object { $_.type }) -join ' OR ' } else { 'desconocida' }
    [pscustomobject]@{ Name = $n; Version = $j.version; License = $lic; Dir = (Split-Path $pj) }
}

# --- Texto ---
$sb = New-Object System.Text.StringBuilder
function Add-Line([string]$s = '') { [void]$sb.Append($s).Append($crlf) }
$sep = '=' * 78

Add-Line 'Componentes con licencia de copyleft (cuyo código fuente se puede obtener en los enlaces):'
Add-Line '  - messagix / mautrix-meta (Messenger e Instagram) · AGPL-3.0 · código fuente: https://github.com/mautrix/meta'
Add-Line '  - go.mau.fi/libsignal (protocolo Signal, usado por WhatsApp) · GPL-3.0 · código fuente: https://pkg.go.dev/go.mau.fi/libsignal'
Add-Line '  - whatsmeow (WhatsApp) · MPL-2.0 · código fuente: https://github.com/tulir/whatsmeow'
Add-Line '  - go.mau.fi/util, go.mau.fi/zeroconfig y maunium.net/go/mautrix · MPL-2.0 · https://github.com/mautrix'
Add-Line '  (dConnector enlaza mautrix-meta (AGPL-3.0) y libsignal (GPL-3.0); por eso dConnector se publica entero bajo AGPL-3.0: ver LICENSE.)'
Add-Line
Add-Line 'Otros componentes principales:'
Add-Line '  - gotd/td (Telegram) · MIT · https://github.com/gotd/td'
Add-Line '  - Wails (ventana y bandeja) · MIT · https://github.com/wailsapp/wails'
Add-Line '  - modernc.org/sqlite (base de datos) · BSD-3-Clause · https://gitlab.com/cznic/sqlite'
Add-Line '  - Go (lenguaje y biblioteca estándar) · BSD-3-Clause · https://go.dev'
Add-Line '  - La interfaz usa React y las bibliotecas listadas más abajo.'
Add-Line '  - Sonido de aviso de mensajes (internal/adapter/desktop/winnotify/message.wav): obtenido de Pixabay (licencia de contenido de Pixabay:'
Add-Line '    https://pixabay.com/service/license-summary/). No es software; se menciona por transparencia.'
Add-Line '  - dConnector usa Microsoft Edge WebView2, que Windows 10/11 ya trae instalado; no se distribuye aquí.'
Add-Line
Add-Line $sep
Add-Line '1. MÓDULOS DE GO (los que entran en dConnector.exe y en dconnector-server) · módulo, versión, licencia'
Add-Line $sep
$w = ($goMods | ForEach-Object { $_.Path.Length } | Measure-Object -Maximum).Maximum
foreach ($m in $goMods) {
    Add-Line ('{0}  {1}  {2}' -f $m.Path.PadRight($w), $m.Spdx.PadRight(13), $m.Version)
}
Add-Line '(*) Sin archivo de licencia propio: deriva de golang.org/x/crypto; se aplica el texto BSD-3-Clause de Go.'
Add-Line
Add-Line $sep
Add-Line '2. BIBLIOTECAS DE LA INTERFAZ · paquete, versión, licencia'
Add-Line $sep
$w2 = ($npm | ForEach-Object { $_.Name.Length } | Measure-Object -Maximum).Maximum
foreach ($n in $npm) { Add-Line ('{0}  {1}  {2}' -f $n.Name.PadRight($w2), $n.License.PadRight(13), $n.Version) }
Add-Line
Add-Line $sep
Add-Line '3. TEXTO DE LAS LICENCIAS'
Add-Line $sep

$copyleft = 'AGPL-3.0', 'MPL-2.0', 'LGPL', 'GPL', 'GPL-2.0', 'GPL-3.0'
$conTextoMods = $goMods | Where-Object { ($conTexto -contains $_.Path) -or ($copyleft -contains $_.Spdx) }
foreach ($m in $conTextoMods) {
    Add-Line
    Add-Line ('--- {0} {1} · {2}' -f $m.Path, $m.Version, $m.Spdx)
    if ($enlaces.ContainsKey($m.Path)) { Add-Line ('Código fuente: ' + $enlaces[$m.Path]) }
    Add-Line
    if ($m.Text) { Add-Line $m.Text } else { Add-Line '(sin archivo de licencia en el módulo)' }
}

$goLic = Join-Path $goroot 'LICENSE'
if (Test-Path $goLic) {
    Add-Line
    Add-Line '--- Go (lenguaje y biblioteca estándar) · BSD-3-Clause'
    Add-Line 'Código fuente: https://go.dev/dl/'
    Add-Line
    Add-Line (Read-Text $goLic)
}

# Licencias de la interfaz: texto de cada paquete.
foreach ($n in $npm) {
    $f = Get-LicenseFile $n.Dir
    Add-Line
    Add-Line ('--- {0} {1} · {2}' -f $n.Name, $n.Version, $n.License)
    Add-Line
    if ($f) { Add-Line (Read-Text $f) } else { Add-Line ('(el paquete no trae archivo de licencia; declara ' + $n.License + ' en su package.json)') }
}

# Textos de las licencias que solo están en la tabla: se imprime uno por cada licencia distinta no incluida arriba.
$yaImpresos = @{}
foreach ($m in $conTextoMods) { $yaImpresos[$m.Path] = $true }
$resto = $goMods | Where-Object { -not $yaImpresos.ContainsKey($_.Path) -and $_.Text }
if ($resto) {
    Add-Line
    Add-Line $sep
    Add-Line '4. TEXTO DE LAS LICENCIAS DEL RESTO DE MÓDULOS DE GO (en el orden de la tabla)'
    Add-Line $sep
    foreach ($m in $resto) {
        Add-Line
        Add-Line ('--- {0} {1} · {2}' -f $m.Path, $m.Version, $m.Spdx)
        Add-Line
        Add-Line $m.Text
    }
}

$cuerpo = $sb.ToString()
$utf8 = New-Object System.Text.UTF8Encoding($true)
$cabLic = @('dConnector · avisos de licencias de terceros', 'Copyright (c) 2026 Darkmelon', '',
    'dConnector se publica bajo la licencia GNU AGPL-3.0 (archivo LICENSE). Incluye componentes de terceros, cada uno',
    'bajo su propia licencia. Este archivo reúne el aviso y el texto de esas licencias. La aplicación no tiene telemetría',
    'ni actualizador remoto.', '') -join $crlf
$cabNot = @('dConnector · THIRD-PARTY-NOTICES (avisos de terceros)', 'Copyright (c) 2026 Darkmelon', '',
    'dConnector se publica bajo la licencia GNU AGPL-3.0 (archivo LICENSE). Este archivo reúne los avisos y los textos de',
    'licencia de los componentes de terceros que entran en la aplicación de escritorio y en el servidor. Se genera con',
    'scripts/licencias.ps1 a partir de los módulos reales (no se edita a mano). El mismo contenido viaja como LICENCIAS.txt',
    'en el instalador y en el paquete portable.', '') -join $crlf
[System.IO.File]::WriteAllText($Out, $cabLic + $crlf + $cuerpo, $utf8)
[System.IO.File]::WriteAllText($Notices, $cabNot + $crlf + $cuerpo, $utf8)
$desconocidas = $goMods | Where-Object { $_.Spdx -like 'desconocida*' -or $_.Spdx -like 'sin archivo*' }
Write-Host ("LICENCIAS.txt y THIRD-PARTY-NOTICES: {0} módulos de Go, {1} paquetes de la interfaz, {2:N0} bytes cada uno" -f $goMods.Count, @($npm).Count, (Get-Item $Out).Length)
if ($desconocidas) { Write-Warning ('Sin licencia reconocida: ' + (($desconocidas | ForEach-Object { $_.Path + ' [' + $_.Spdx + ']' }) -join ', ')) }
