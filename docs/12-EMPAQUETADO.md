# 12 · Empaquetado y lanzamiento (0.3.0)

`task release` produce TODO en `dist/` desde una sola máquina Windows, sin CGO. Los paquetes del servidor y de Linux
se pueden producir también en un runner Linux (solo `go` y `task`).

## 1. Herramientas

| Para | Necesita |
|---|---|
| Todo lo del servidor, echo-server, `checksums`, `release:linux`, `docker` | `go` y `task` (y `docker` para la imagen) |
| Escritorio de Windows (`build:desktop`, `portable`, `msi`, `release:windows`, `gate`) | además `wails3` (v3.1.0), `pnpm` y `node` 22; Windows PowerShell 5.1 |
| `msi` | `wix` 5.0.2 (`dotnet tool install --global wix --version 5.0.2`) con las extensiones `wix extension add -g WixToolset.UI.wixext/5.0.2` y `wix extension add -g WixToolset.Util.wixext/5.0.2` |

Instalación: `go install github.com/go-task/task/v3/cmd/task@latest` y
`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`. El `PATH` debe incluir `%USERPROFILE%\go\bin` y
`%USERPROFILE%\.dotnet\tools`. `CGO_ENABLED=0` ya lo fija el Taskfile.

La versión sale de `VERSION` (a.b.c). Para una prueba: `task release VERSION=0.3.0-prueba` (los nombres llevan el
sufijo completo; el MSI usa solo `a.b.c`). `build/windows/info.json` es la plantilla: `scripts/info.ps1` escribe el
real en `bin/obj/info.json` con la `VERSION` al construir (no hay que subirlo a mano).

## 2. Tareas

| Tarea | Qué hace |
|---|---|
| `build:desktop` (`ARCH=x86\|x64\|arm64`, por defecto x64) | `bin/windows-<ARCH>/dConnector.exe`: `-tags production -H windowsgui`, con el recurso de Windows (`wails3 generate syso -arch 386\|amd64\|arm64`; el `.syso` se coloca en `cmd/dconnector/` y se borra después) |
| `build:server` (`GOOS`, `GOARCH`) | `bin/server/<GOOS>-<GOARCH>/dConnector-server.exe` (Windows) o `dconnector-server` (Linux); GOARCH `amd64`, `arm64` o `386` |
| `build:echo` (`GOOS`, `GOARCH`) | `bin/echo/<GOOS>-<GOARCH>/echo-server[.exe]` |
| `build` | desarrollo: `bin/dConnector.exe` (x64) y `bin/dConnector-server.exe` juntos |
| `msi`, `portable` (`ARCH`) | un MSI / un zip portable, con su comprobación. `msi:all` y `portable:all` recorren `DESKTOP_ARCHS` (por defecto `x64 arm64`) |
| `package:server`, `package:echo` (`GOOS`, `GOARCH`) | empaquetan el servidor / echo-server en `dist/` |
| `release:windows` | escritorio de `DESKTOP_ARCHS` (MSI + zip), servidor Windows x64/arm64, echo-server Windows x64/arm64 |
| `release:linux` | servidor y echo-server Linux x64/arm64 (`tar.gz`) |
| `checksums` | `dist/SHA256SUMS` y su verificación |
| `docker` | construye `deploy/Dockerfile` y lo arranca con su `healthcheck`; sin Docker avisa y sale con 0; no publica |
| `release` | `clean-dist` + `release:windows` + `release:linux` + `checksums` |
| `test` | `gofmt -l` vacío, `go vet`, `go test ./...`, `tsc --noEmit`, `vitest run` |
| `check:cross` | compila (sin guardar) servidor linux amd64/arm64 y windows 386/amd64/arm64, y escritorio windows 386/amd64/arm64 |
| `selftest`, `version` | comprueban `bin/dConnector.exe` (con `--require-server`) |
| `gate` | `test` + `build` + `version` + `selftest` + `check:cross`; deja `bin/dConnector.exe` y `bin/dConnector-server.exe` |
| `clean-dist` | borra `dist/` |

`scripts/dist.go` (solo biblioteca estándar, con la etiqueta `ignore`) hace lo multiplataforma: paquetes del servidor y
echo-server, `SHA256SUMS`, `gofmt`, copias y la prueba Docker. Los `.ps1` (`msi`, `portable`, `selftest`, `info`, `lib`)
son solo de Windows.

Banderas: `-trimpath -buildvcs=false -ldflags "-s -w -buildid= -X dconnector/internal/buildinfo.Version=<VERSION>"`.
Ejecutables, zip y `tar.gz` son reproducibles (marcas de tiempo fijas, sin propietario; `SOURCE_DATE_EPOCH` las cambia).
El MSI no lo es (WiX escribe GUID y fechas).

## 3. Artefactos de `dist/` (todos sueltos en el primer nivel)

`<arch>`: `x86`, `x64`, `arm64`.

| Archivo | Contenido |
|---|---|
| `dConnector-<ver>-<arch>.msi` | instalador por usuario (3 arquitecturas) |
| `dConnector-<ver>-portable-<arch>.zip` | carpeta `dConnector-<ver>-portable-<arch>/` con `dConnector.exe`, `dConnector-server.exe`, `portable.txt`, `LEEME.txt`, `LICENCIAS.txt` (+ `LICENSE`, `THIRD-PARTY-NOTICES`) |
| `dConnector-server-<ver>-windows-<x64\|arm64>.zip` | `dConnector-server.exe`, `LEEME-SERVIDOR.txt`, `LICENCIAS.txt` (+ licencias) |
| `dConnector-server-<ver>-linux-<x64\|arm64>.tar.gz` | `dconnector-server` (0755), `dconnector.service`, `dconnector.env.example`, `LEEME-SERVIDOR.txt`, `LICENCIAS.txt` (+ licencias), bajo una carpeta raíz del mismo nombre |
| `echo-server-<ver>-windows-<x64\|arm64>.zip` | carpeta con `echo-server.exe`, `LICENCIAS.txt` (+ `LICENSE`, `THIRD-PARTY-NOTICES`): enlaza `coder/websocket` (ISC), cuyo aviso debe acompañar al binario |
| `echo-server-<ver>-linux-<x64\|arm64>.tar.gz` | `echo-server` (0755) y las mismas licencias, bajo una carpeta raíz |
| `SHA256SUMS` | `hash  nombre`, orden alfabético, solo los artefactos de la versión |

`LICENSE` y `THIRD-PARTY-NOTICES` (de la raíz) se incluyen si existen; si faltan se avisa y se sigue. Los
`LEEME-SERVIDOR.txt` salen de `build/server/` (se sustituye `@VERSION@`).

## 4. MSI (`scripts/msi.ps1`, `build/windows/msi/dConnector.wxs`)

- WiX 5.0.2, `-arch x86|x64|arm64`, `Scope="perUser"` (instala en `%LOCALAPPDATA%\Programs\dConnector`, sin administrador).
- **Mismo `UpgradeCode` `{637254E9-11AA-4BEB-A2EE-BDB33F6347EA}`** en las tres: actualiza encima de 0.1.x/0.2.x. El GUID del
  componente es distinto por arquitectura; el de x64 (`AB6CADB7-…`) es el histórico y no se cambia nunca.
- **Instalar otra arquitectura encima (mismo `UpgradeCode`)**: Windows Installer rechaza un paquete que su procesador no admite
  (el MSI arm64 no se instala en un Windows x64). Con la MISMA versión, el segundo MSI se detiene con «Ya está instalada una
  versión más nueva» (la regla `Upgrade` detecta la versión igual): hay que desinstalar el primero. Con una versión MÁS NUEVA de otra
  arquitectura, el MSI quita el anterior y deja el nuevo (el GUID del componente cambia, no se mezclan archivos); en un Windows arm64
  eso sirve para pasar de x64 (emulado) a arm64 nativo, pero un MSI x64 posterior también reemplazaría a uno arm64: use siempre el MSI
  de la arquitectura de su equipo. El MSI x86 no se instala en un Windows de 64 bits (condición de lanzamiento), para que nadie
  reemplace sin querer un x64 por el escritorio de 32 bits, que además no arranca (sección 8).
- Instala `dConnector.exe`, `dConnector-server.exe` (misma arquitectura), `LEEME.txt`, `LICENCIAS.txt` (+ licencias).
- Antes de reemplazar o quitar archivos, `util:CloseApplication` (RestartManager) cierra `dConnector.exe` y termina
  `dConnector-server.exe` por nombre: no queda el servidor corriendo tras actualizar ni desinstalar. Un servicio de Windows
  registrado con `--service install` es de sistema: se detiene con `--service stop` antes de desinstalar (el MSI por usuario
  no puede hacerlo).
- Desinstalar **no** toca `%APPDATA%\dConnector` (datos y sesiones).
- Si `-culture es-ES` falla, se compila sin él (diálogos en inglés) con un aviso.

Comprobaciones automáticas (sin instalar nada): tablas (`UpgradeCode`, `ProductVersion`, plataforma, perUser, rango de
`Upgrade` [0.0.0, versión) y bloqueo de las más nuevas, `RemoveExistingProducts`, tabla de cierre con los dos exe, GUID del
componente, lista de archivos y tamaños, registro solo en `HKCU\Software\Darkmelon\dConnector`, ninguna carpeta de
AppData), encabezado PE de cada exe (`0x14C`, `0x8664`, `0xAA64`) y, si la máquina corre esa arquitectura, extracción
administrativa (`msiexec /a`) y, sobre lo extraído, `--version` y `--selftest --require-server`.

## 5. Portable (`scripts/portable.ps1`)

El zip se escribe con `System.IO.Compression` (rutas con `/`, una carpeta raíz, orden y fechas fijos; la carpeta se arma en
una temporal, no queda en `dist/`). Se comprueba: carpeta raíz única, contenido exacto, PE de cada exe y, si la máquina
corre esa arquitectura, descomprimirlo en una carpeta nueva y `--version` + `--selftest --require-server` con
`portable: true` y la carpeta `data` junto al exe.

## 6. Qué se ejecuta y qué solo se verifica en estructura

En Windows x64: x64 y x86 (32 bits) se EJECUTAN (extracción del MSI, `--version`, `--selftest`). arm64 solo se verifica en
estructura (tablas del MSI, PE `0xAA64` del escritorio y del servidor, tamaños). Linux se verifica leyendo el `tar.gz` de
vuelta (modos, rutas con `/`, contenido igual al origen, ELF `x86-64` / `aarch64`). La ejecución en arm64 y Linux, la
instalación real del MSI y la imagen Docker (si no hay Docker) se prueban en CI.

## 7. Versión

`VERSION` y `frontend/package.json` se suben juntos; `info.json` y el MSI siguen a `VERSION` solos. Al terminar: commit
`release: dConnector <versión>` y etiqueta `v<versión>` (el workflow de GitHub sube `dist/` al Release).

## 8. x86: compila, pero el escritorio no arranca (Wails v3.1.0)

`GOARCH=386` compila el escritorio y el servidor, y el MSI x86 se arma bien, pero `dConnector.exe` de 32 bits entra en
pánico al arrancar (`compileCallback: argument size is larger than uintptr`, en `wails/v3/pkg/w32` `init`: las
tablas COM de Wails solo valen para 64 bits). El servidor x86 sí funciona (`--version` OK). Por eso `DESKTOP_ARCHS` es
`x64 arm64` y `release` no publica x86. `task release DESKTOP_ARCHS="x64 x86 arm64"` (o `task msi ARCH=x86`) lo arma, y su
comprobación de ejecución falla a propósito. Cuando Wails lo arregle, basta cambiar el valor por defecto en el Taskfile.
