# Compila el instalador MSI de dConnector de UNA arquitectura (docs/12-EMPAQUETADO.md) y lo comprueba sin instalar nada.
# Uso: scripts\msi.ps1 [-Arch x64|x86|arm64] [-Version a.b.c[-sufijo]] [-Exe <dConnector.exe>] [-ServerExe <dConnector-server.exe>] [-Scratch <carpeta>]
# Requiere en el PATH: wix 5.0.2 con las extensiones WixToolset.UI.wixext y WixToolset.Util.wixext (5.0.2).
# Salida: dist\dConnector-<versión>-<arch>.msi. Se puede repetir: borra y recrea su salida. No deja procesos vivos.
# Se EJECUTA (extracción administrativa, --version, --selftest) solo lo que esta máquina puede correr; arm64 se verifica
# estructuralmente (tablas y encabezados PE de lo que contiene).
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
$wxs = Join-Path $root 'build\windows\msi\dConnector.wxs'
$iconPath = Join-Path $root 'build\windows\icon.ico'
$licenseRtf = Join-Path $root 'build\windows\msi\License.rtf'
$licensesTxt = Join-Path $root 'LICENCIAS.txt'
$leemeTxt = Join-Path $root 'LEEME.txt'
foreach ($f in $wxs, $iconPath, $licenseRtf, $licensesTxt, $leemeTxt) { if (-not (Test-Path $f)) { throw "falta $f" } }

# GUID del componente por arquitectura. El de x64 es el de 0.1.x/0.2.x: no se cambia nunca (si no, actualizar encima duplica archivos).
$guids = @{ 'x64' = 'AB6CADB7-96B9-45A0-AB8A-6A76C6F88CAC'; 'x86' = 'D4D44129-2E17-4D6D-BEAC-BA552EF1D315'; 'arm64' = '1864A170-946C-4DBC-A528-20CFC05B759A' }
$expectedUpgradeCode = '{637254E9-11AA-4BEB-A2EE-BDB33F6347EA}'
$dist = Join-Path $root 'dist'
$msi = Join-Path $dist "dConnector-$version-$Arch.msi"
New-Item -ItemType Directory -Force $dist | Out-Null
if (Test-Path $msi) { Remove-Item -Force $msi }

Write-Host "== MSI $Arch · $version =="
Assert-Machine $exePath $info
Assert-Machine $serverPath $info

# --- Compilar ---
$docs = @(Get-OptionalDocs $root)
function Invoke-WixBuild([bool]$conCultura) {
    $a = @('build', $wxs, '-arch', $Arch, '-ext', 'WixToolset.UI.wixext', '-ext', 'WixToolset.Util.wixext')
    if ($conCultura) { $a += @('-culture', 'es-ES') }
    $a += @('-pdbtype', 'none', '-d', "Version=$($v.Num)", '-d', "ExePath=$exePath", '-d', "ServerExePath=$serverPath", '-d', "IconPath=$iconPath",
            '-d', "LicenseRtf=$licenseRtf", '-d', "LicensesTxt=$licensesTxt", '-d', "LeemeTxt=$leemeTxt", '-d', "ComponentGuid=$($guids[$Arch])")
    foreach ($d in $docs) {
        if ((Split-Path $d -Leaf) -eq 'LICENSE') { $a += @('-d', "LicenseTxt=$d") } else { $a += @('-d', "ThirdPartyTxt=$d") }
    }
    $a += @('-o', $msi)
    $out = & wix @a 2>&1 | Out-String
    return [pscustomobject]@{ Code = $LASTEXITCODE; Out = $out }
}
$r = Invoke-WixBuild $true
$cultura = 'es-ES'
if ($r.Code -ne 0) {
    Write-Warning "wix build con -culture es-ES falló; se reintenta sin -culture (diálogos en inglés):`n$($r.Out)"
    $r = Invoke-WixBuild $false
    $cultura = '(sin -culture: diálogos en inglés)'
}
if ($r.Code -ne 0) { throw "wix build falló:`n$($r.Out)" }
if ($r.Out.Trim()) { Write-Host $r.Out.Trim() }
if (-not (Test-Path $msi)) { throw "wix no produjo $msi" }
Write-Host "Creado: $msi ($([math]::Round((Get-Item $msi).Length / 1MB, 1)) MB) · cultura: $cultura"

# --- Lectura de tablas del MSI (WindowsInstaller.Installer, sin instalar nada) ---
function Open-MsiDb([string]$path) {
    $inst = New-Object -ComObject WindowsInstaller.Installer
    return $inst.GetType().InvokeMember('OpenDatabase', 'InvokeMethod', $null, $inst, @($path, 0))
}
function Get-MsiRows($db, [string]$sql, [int]$cols) {
    $vw = $db.GetType().InvokeMember('OpenView', 'InvokeMethod', $null, $db, @($sql))
    $vw.GetType().InvokeMember('Execute', 'InvokeMethod', $null, $vw, $null) | Out-Null
    $rows = @()
    while ($true) {
        $rec = $vw.GetType().InvokeMember('Fetch', 'InvokeMethod', $null, $vw, $null)
        if ($null -eq $rec) { break }
        $rows += , @(for ($i = 1; $i -le $cols; $i++) { [string]$rec.GetType().InvokeMember('StringData', 'GetProperty', $null, $rec, @($i)) })
    }
    $vw.GetType().InvokeMember('Close', 'InvokeMethod', $null, $vw, $null) | Out-Null
    return , $rows
}

Write-Host 'Comprobando las tablas del MSI:'
$db = Open-MsiDb $msi
$props = @{}
foreach ($row in (Get-MsiRows $db 'SELECT `Property`, `Value` FROM `Property`' 2)) { $props[$row[0]] = $row[1] }
Assert-True ($props['UpgradeCode'] -eq $expectedUpgradeCode) "UpgradeCode = $($props['UpgradeCode'])"
Assert-True ($props['ProductVersion'] -eq $v.Num) "ProductVersion = $($props['ProductVersion']) (VERSION: $version)"
Assert-True ($props['ProductName'] -eq 'dConnector' -and $props['Manufacturer'] -eq 'Darkmelon') 'ProductName dConnector, Manufacturer Darkmelon'
Assert-True ($props['ProductLanguage'] -eq '3082') 'ProductLanguage 3082 (es-ES)'

# Plataforma del paquete (Template del resumen) e instalación por usuario (bit 8 del «Word Count»).
$si = $db.GetType().InvokeMember('SummaryInformation', 'GetProperty', $null, $db, @(0))
$template = [string]$si.GetType().InvokeMember('Property', 'GetProperty', $null, $si, @(7))
$wordCount = [int]$si.GetType().InvokeMember('Property', 'GetProperty', $null, $si, @(15))
Assert-True ($template -like "$($info.Template);*") "plataforma del paquete = '$template' (esperada $($info.Template))"
Assert-True (-not $props.ContainsKey('ALLUSERS')) 'no hay ALLUSERS (no es por equipo)'
Assert-True (($wordCount -band 8) -eq 8) "Word Count = ${wordCount}: bit 8 = no pide privilegios de administrador (Scope perUser)"

# Actualizar encima de 0.2.x: mismo UpgradeCode, rango [0.0.0, esta versión) que se quita, y rango de versiones más nuevas solo detectado.
$upg = Get-MsiRows $db 'SELECT `UpgradeCode`, `VersionMin`, `VersionMax`, `Attributes`, `ActionProperty` FROM `Upgrade`' 5
$mine = @($upg | Where-Object { $_[0] -eq $expectedUpgradeCode })
Assert-True ($mine.Count -ge 1) "tabla Upgrade con el mismo UpgradeCode ($(@($upg).Count) filas)"
Write-Host ("  info · filas de Upgrade: " + (($mine | ForEach-Object { "[min=$($_[1]) max=$($_[2]) attr=$($_[3]) prop=$($_[4])]" }) -join " "))
$older = @($mine | Where-Object { ($_[1] -eq '' -or $_[1] -eq '0.0.0') -and $_[2] -eq $v.Num -and ([int]$_[3] -band 2) -eq 0 })
Assert-True ($older.Count -eq 1) "Upgrade quita las versiones [0.0.0, $($v.Num)) del mismo UpgradeCode (incluye 0.1.x y 0.2.x)"
$newer = @($mine | Where-Object { $_[1] -eq $v.Num -and ([int]$_[3] -band 2) -eq 2 })
Assert-True ($newer.Count -eq 1) 'Upgrade solo detecta (y bloquea) versiones más nuevas'
$seq = Get-MsiRows $db 'SELECT `Action`, `Sequence` FROM `InstallExecuteSequence`' 2
$remSeq = @($seq | Where-Object { $_[0] -eq 'RemoveExistingProducts' })
Assert-True ($remSeq.Count -eq 1) 'RemoveExistingProducts está en la secuencia de instalación'

# Cerrar la aplicación y el servidor local antes de reemplazar o quitar archivos.
$tabRows = Get-MsiRows $db 'SELECT `Name` FROM `_Tables`' 1
$tables = @($tabRows | ForEach-Object { $_[0] })
$closeTable = @($tables | Where-Object { $_ -match 'CloseApplication$' })
Assert-True ($closeTable.Count -eq 1) "existe la tabla $($closeTable -join ',') (cierre de aplicaciones)"
$closeRows = Get-MsiRows $db "SELECT ``Target`` FROM ``$($closeTable[0])``" 1
Assert-True (@($closeRows | Where-Object { $_[0] -eq 'dConnector-server.exe' }).Count -eq 1 -and @($closeRows | Where-Object { $_[0] -eq 'dConnector.exe' }).Count -eq 1) 'se cierran dConnector.exe y dConnector-server.exe por nombre antes de reemplazar/quitar'
$closeSeq = @($seq | Where-Object { $_[0] -match 'CloseApplications(_\w+)?$' })
Assert-True ($closeSeq.Count -ge 1) 'la acción de cierre está en la secuencia de instalación (también corre al desinstalar y actualizar)'
Assert-True ([int]$closeSeq[0][1] -lt [int]$remSeq[0][1]) "el cierre ($($closeSeq[0][1])) corre ANTES de quitar la versión anterior ($($remSeq[0][1]), horario afterInstallExecute)"

# La desinstalación no debe tocar %APPDATA%\dConnector: nada en carpetas de datos del usuario.
$dirs = Get-MsiRows $db 'SELECT `Directory`, `Directory_Parent`, `DefaultDir` FROM `Directory`' 3
$malas = @($dirs | Where-Object { $_[0] -match 'AppData|Personal|Local|Common' -or $_[1] -match 'AppData|Personal|Local|Common' })
Assert-True ($malas.Count -eq 0) 'ninguna carpeta del MSI cuelga de AppData (los datos de %APPDATA%\dConnector no se tocan)'
$comps = Get-MsiRows $db 'SELECT `Component`, `Directory_`, `ComponentId` FROM `Component`' 3
Assert-True (@($comps | Where-Object { $_[1] -ne 'INSTALLFOLDER' }).Count -eq 0 -and @($comps).Count -eq 1) 'un solo componente, en INSTALLFOLDER (carpeta del programa)'
Assert-True ($comps[0][2] -eq "{$($guids[$Arch])}") "GUID del componente de $Arch = $($comps[0][2])"

# Archivos: el escritorio, el servidor de la misma arquitectura, LEEME, LICENCIAS y (si existen) LICENSE y THIRD-PARTY-NOTICES.
$files = Get-MsiRows $db 'SELECT `FileName`, `FileSize` FROM `File`' 2
$names = @($files | ForEach-Object { ($_[0] -split '\|')[-1] } | Sort-Object)
$want = @('dConnector.exe', 'dConnector-server.exe', 'LEEME.txt', 'LICENCIAS.txt') + @($docs | ForEach-Object { Split-Path $_ -Leaf }) | Sort-Object
Assert-True (($names -join '|') -eq ($want -join '|')) "el MSI instala: $($names -join ', ')"
$sz = @{}
foreach ($f in $files) { $sz[(($f[0] -split '\|')[-1])] = [long]$f[1] }
Assert-True ($sz['dConnector.exe'] -eq (Get-Item $exePath).Length -and $sz['dConnector-server.exe'] -eq (Get-Item $serverPath).Length) 'tamaños de los dos ejecutables = los de bin\ (los PE de arriba son los que contiene)'
$regs = Get-MsiRows $db 'SELECT `Root`, `Key` FROM `Registry`' 2
Assert-True (@($regs | Where-Object { $_[0] -ne '1' -or $_[1] -notmatch '^Software\\Darkmelon\\dConnector$' }).Count -eq 0) 'el registro solo escribe HKCU\Software\Darkmelon\dConnector'
$rf = Get-MsiRows $db 'SELECT `DirProperty`, `InstallMode` FROM `RemoveFile`' 2
Assert-True (@($rf | Where-Object { $_[0] -ne 'INSTALLFOLDER' }).Count -eq 0) 'RemoveFile/RemoveFolder solo actúa sobre INSTALLFOLDER'
$dlg = Get-MsiRows $db 'SELECT `Title` FROM `Dialog` WHERE `Dialog`=''WelcomeEulaDlg''' 1
Write-Host "  info · título del diálogo de bienvenida: '$($dlg[0][0])'"
$db = $null; [GC]::Collect(); [GC]::WaitForPendingFinalizers()

# --- Extracción administrativa (no instala nada) y pruebas de ejecución, si esta máquina corre esa arquitectura ---
if (-not (Test-CanRun $Arch)) {
    Write-Host "Solo verificación estructural: esta máquina no ejecuta $Arch (sin extracción administrativa ni --selftest)."
    Write-Host "msi OK (estructural): $msi"
    return
}
if (-not $Scratch) { $Scratch = Join-Path ([System.IO.Path]::GetTempPath()) ('dc-msi-' + [guid]::NewGuid().ToString('N')) }
$target = Join-Path $Scratch 'msi-test'
$dataDir = Join-Path $Scratch 'msi-test-data'
foreach ($d in $target, $dataDir) { if (Test-Path $d) { Remove-Item -Recurse -Force $d } }
New-Item -ItemType Directory -Force $target, $dataDir | Out-Null

$log = Join-Path $Scratch 'msiexec.log'
$p = Start-Process -FilePath 'msiexec.exe' -ArgumentList @('/a', "`"$msi`"", '/qn', "TARGETDIR=`"$target`"", '/l*v', "`"$log`"") -Wait -PassThru
if ($p.ExitCode -ne 0) { throw "msiexec /a falló (código $($p.ExitCode)); registro: $log" }
$inner = Get-ChildItem -Path $target -Recurse -Filter 'dConnector.exe' -File | Select-Object -First 1
if (-not $inner) { throw "No hay dConnector.exe dentro de la extracción en $target" }
$innerDir = $inner.DirectoryName
foreach ($f in 'dConnector-server.exe', 'LEEME.txt', 'LICENCIAS.txt') { if (-not (Test-Path (Join-Path $innerDir $f))) { throw "La extracción no contiene $f" } }
Write-Host "Extracción administrativa OK: $innerDir"
Assert-Machine $inner.FullName $info
Assert-Machine (Join-Path $innerDir 'dConnector-server.exe') $info

$null = Test-Package $innerDir $version $Scratch @{ DCONNECTOR_DATA = $dataDir }

Stop-StrayProcesses $Scratch
Remove-Item -Recurse -Force $Scratch -ErrorAction SilentlyContinue
Write-Host "msi OK: $msi"
