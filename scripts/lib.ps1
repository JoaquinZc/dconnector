# Funciones comunes de msi.ps1, portable.ps1 y selftest.ps1 (se cargan con «. $PSScriptRoot\lib.ps1»).

# Arquitecturas de los paquetes de escritorio: x86, x64 y arm64.
function Get-ArchInfo([string]$Arch) {
    switch ($Arch) {
        'x86'   { return [pscustomobject]@{ Arch = 'x86';   GoArch = '386';   Syso = '386';   PeMachine = 0x14C;  Template = 'Intel' } }
        'x64'   { return [pscustomobject]@{ Arch = 'x64';   GoArch = 'amd64'; Syso = 'amd64'; PeMachine = 0x8664; Template = 'x64' } }
        'arm64' { return [pscustomobject]@{ Arch = 'arm64'; GoArch = 'arm64'; Syso = 'arm64'; PeMachine = 0xAA64; Template = 'Arm64' } }
        default { throw "Arquitectura desconocida '$Arch' (use x86, x64 o arm64)" }
    }
}

# VERSION completa (a.b.c o a.b.c-sufijo) y su parte numérica a.b.c (la que admite un MSI).
function Get-VersionInfo([string]$Version, [string]$Root) {
    if (-not $Version) { $Version = (Get-Content (Join-Path $Root 'VERSION') -Raw).Trim() }
    if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.]+)?$') { throw "VERSION no es a.b.c[-sufijo]: '$Version'" }
    if ([int]$Matches[1] -gt 255 -or [int]$Matches[2] -gt 255 -or [int]$Matches[3] -gt 65535) { throw "VERSION fuera del rango de un MSI (255.255.65535): $Version" }
    return [pscustomobject]@{ Full = $Version; Num = "$($Matches[1]).$($Matches[2]).$($Matches[3])" }
}

# «Máquina» del encabezado PE de un ejecutable (0x14C x86, 0x8664 x64, 0xAA64 arm64).
function Get-PeMachine([string]$Path) {
    $fs = [System.IO.File]::OpenRead($Path)
    try {
        $br = New-Object System.IO.BinaryReader($fs)
        if ($br.ReadUInt16() -ne 0x5A4D) { throw "$Path no es un ejecutable de Windows (falta MZ)" }
        $fs.Position = 0x3C
        $pe = $br.ReadUInt32()
        $fs.Position = $pe
        if ($br.ReadUInt32() -ne 0x4550) { throw "${Path}: encabezado PE inválido" }
        return [int]$br.ReadUInt16()
    } finally { $fs.Dispose() }
}

function Assert-Machine([string]$Path, $Info) {
    $m = Get-PeMachine $Path
    if ($m -ne $Info.PeMachine) { throw ("{0}: máquina PE 0x{1:X} y se esperaba 0x{2:X} ({3})" -f $Path, $m, $Info.PeMachine, $Info.Arch) }
    Write-Host ("  ok · {0}: PE 0x{1:X} ({2})" -f (Split-Path $Path -Leaf), $m, $Info.Arch)
}

# ¿Esta máquina puede ejecutar ejecutables de esa arquitectura? (x64 ejecuta x86; arm64 ejecuta x86 y x64)
function Test-CanRun([string]$Arch) {
    $native = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    switch ($native) {
        'X64'   { return ($Arch -eq 'x64' -or $Arch -eq 'x86') }
        'Arm64' { return $true }
        'X86'   { return ($Arch -eq 'x86') }
        default { return $false }
    }
}

function Assert-True([bool]$cond, [string]$msg) {
    if (-not $cond) { throw "Comprobación falló: $msg" }
    Write-Host "  ok · $msg"
}

# Ejecuta un exe con --out (con -H windowsgui la consola se pierde) y devuelve código y texto.
function Invoke-WithOut([string]$exe, [string[]]$exeArgs, [string]$outFile) {
    if (Test-Path $outFile) { Remove-Item -Force $outFile }
    $pr = Start-Process -FilePath $exe -ArgumentList ($exeArgs + @('--out', "`"$outFile`"")) -WorkingDirectory (Split-Path $exe) -Wait -PassThru -WindowStyle Hidden
    $text = if (Test-Path $outFile) { Get-Content $outFile -Raw } else { '' }
    return [pscustomobject]@{ Code = $pr.ExitCode; Text = $text }
}

# Comprobaciones de ejecución de un escritorio y su servidor ya extraídos (misma carpeta).
function Test-Package([string]$Dir, [string]$Version, [string]$Scratch, [hashtable]$ExtraEnv) {
    $exe = Join-Path $Dir 'dConnector.exe'
    $srv = Join-Path $Dir 'dConnector-server.exe'
    foreach ($e in $ExtraEnv.GetEnumerator()) { Set-Item -Path "Env:\$($e.Key)" -Value $e.Value }
    try {
        $r = Invoke-WithOut $srv @('--version') (Join-Path $Scratch 'srv-version.txt')
        Write-Host "dConnector-server --version -> '$($r.Text.Trim())' (VERSION: '$Version')"
        if ($r.Code -ne 0 -or $r.Text.Trim() -ne $Version) { throw 'La versión del servidor no coincide con VERSION' }
        $r = Invoke-WithOut $exe @('--version') (Join-Path $Scratch 'version.txt')
        Write-Host "dConnector --version -> '$($r.Text.Trim())' (VERSION: '$Version')"
        if ($r.Code -ne 0 -or $r.Text.Trim() -ne $Version) { throw 'La versión del escritorio no coincide con VERSION' }
        $r = Invoke-WithOut $exe @('--selftest', '--require-server') (Join-Path $Scratch 'selftest.txt')
        Write-Host $r.Text
        if ($r.Code -ne 0 -or $r.Text -notmatch '(?m)^OK') { throw "selftest --require-server falló (código $($r.Code))" }
        return $r.Text
    } finally {
        foreach ($e in $ExtraEnv.Keys) { Remove-Item -Path "Env:\$e" -ErrorAction SilentlyContinue }
    }
}

function Stop-StrayProcesses([string]$Scratch) {
    $vivos = Get-Process -ErrorAction SilentlyContinue | Where-Object { ($_.Name -eq 'dConnector' -or $_.Name -eq 'dConnector-server') -and $_.Path -and $_.Path -like "$Scratch*" }
    if ($vivos) { $vivos | Stop-Process -Force; throw 'Quedó un proceso dConnector o dConnector-server vivo tras la prueba' }
}

# Archivos de licencia opcionales de la raíz (los crea el agente de documentación).
function Get-OptionalDocs([string]$Root) {
    $r = @()
    foreach ($f in 'LICENSE', 'THIRD-PARTY-NOTICES') {
        $p = Join-Path $Root $f
        if (Test-Path $p) { $r += $p } else { Write-Warning "no existe $f en la raíz; no se incluye en el paquete." }
    }
    return $r
}
