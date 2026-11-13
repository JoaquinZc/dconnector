# Integración continua y publicación (GitHub Actions)

Los flujos viven en `.github/workflows/`. No usan secretos: solo el `GITHUB_TOKEN` que da GitHub, con
`contents: read` por defecto.

## `ci.yml` (cada push a `main`/`master`, cada PR y ejecución manual)

Un push nuevo a la misma rama cancela la ejecución anterior.

| Job | Runner | Qué hace |
| --- | --- | --- |
| `linux` (x64 y arm64) | `ubuntu-24.04`, `ubuntu-24.04-arm` | `gofmt`, `go vet`, `staticcheck`, `go test`, `go test -race` (servidor, API, núcleo), compila el servidor, comprueba `--version`, `--selftest` y una prueba E2E con `healthcheck` y parada limpia, todo en carpetas temporales. Aquí se prueban de verdad Linux x64 y arm64. |
| `docker` | `ubuntu-24.04` | Construye `deploy/Dockerfile` y espera a que el HEALTHCHECK quede «healthy». No publica. |
| `windows` | `windows-latest` | Instala `wails3` v3.1.0, `task` y `staticcheck` con versión fija; `staticcheck ./...`; `task gate` (vet, pruebas de Go, `tsc`, `vitest`, compilación con `-tags production`, `--version`, `--selftest`); y el escritorio con el servidor al lado (`--selftest --require-server`). |
| `windows-arm64` | `windows-11-arm` | Pruebas de los paquetes sin Wails y selftest nativo del servidor arm64. Es un runner reciente: el job no bloquea la integración (`continue-on-error`). |

Limitación de Linux: `cmd/dconnector` e `internal/adapter/ui/wails` (y el paquete `frontend`, que embebe
`dist/`) necesitan Wails, que en Linux exige CGO y GTK/WebKit. Solo se compilan en Windows; en Linux se
excluyen con `go list ./... | grep -v …`.

## `release.yml` (al empujar una etiqueta `v*`)

1. `verify`: la etiqueta debe ser `v` + el contenido de `VERSION` (que debe ser `a.b.c[-sufijo]`) y el commit etiquetado debe estar
   en la rama principal del repositorio (`git merge-base --is-ancestor`). Si no, falla antes de compilar y no se publica nada.
   Ningún valor de `github.event.*` se interpola dentro de un script: el nombre de la rama llega por una variable de entorno y
   la etiqueta por `GITHUB_REF_NAME`, ambas siempre entre comillas.
2. `windows`: `task release:windows` (MSI x64/arm64, zips portables, servidor Windows x64/arm64, echo-server).
3. `linux`: `task release:linux` (servidor y echo-server Linux x64/arm64, compilados de forma cruzada).
4. `linux-arm64-native`: extrae el `tar.gz` arm64 y ejecuta `--version`, `--selftest` y `healthcheck` en un runner arm64 real.
5. `docker`: construye la imagen y prueba su healthcheck (sin publicar).
6. `publish` (único job con `contents: write`): descarga todos los artefactos, comprueba que están los 12 archivos esperados
   (MSI, zip portable, servidor y echo-server de Windows y Linux, x64 y arm64) y ninguno más, y SOLO ENTONCES genera
   `SHA256SUMS` con todos (orden estable) y lo verifica con `sha256sum --check --strict`. El Release se crea como BORRADOR, se
   suben los archivos y por último se publica (`--draft=false`): un fallo a medias no deja un Release público incompleto. Las notas salen de `docs/RELEASE-NOTES-<etiqueta>.md`
   si existe; si no, de `--generate-notes`. Si el Release ya existe (reejecución), sube los archivos con
   `--clobber` (solo reemplaza los activos de ese mismo Release) y actualiza las notas. Una etiqueta con guion (`v0.3.1-rc1`) se publica como prerrelease.

### Cómo lanzar un release

1. Suba `VERSION` (y `info.json`/`package.json`) a la versión nueva y fusione a la rama principal.
2. Opcional: escriba `docs/RELEASE-NOTES-v<versión>.md`.
3. `git tag v<versión>` y `git push origin v<versión>`.
4. Vigile la ejecución en la pestaña Actions. Si falla, corrija, mueva o cree una etiqueta nueva y repita.

Suposiciones sobre las tareas del `Taskfile.yml`: `release:windows` y `release:linux` dejan en `dist/` archivos
sueltos con los nombres `dConnector-<ver>-<arch>.msi`, `dConnector-<ver>-portable-<arch>.zip`,
`dConnector-server-<ver>-windows-<arch>.zip`, `dConnector-server-<ver>-linux-<arch>.tar.gz` y
`echo-server-<ver>-windows-<arch>.zip` y `echo-server-<ver>-linux-<arch>.tar.gz`; el tar.gz de Linux contiene el binario `dconnector-server`.
Solo se suben los archivos del primer nivel de `dist/` (no las carpetas portables).

## Publicar la imagen Docker en GHCR

Hoy la imagen solo se construye y se prueba. Para publicarla en `ghcr.io/<propietario>/<repositorio>`:

1. En `release.yml`, job `docker`, añada `permissions: { contents: read, packages: write }`.
2. Descomente el paso «Publicar en GHCR» (inicia sesión con `GITHUB_TOKEN`, etiqueta con la versión y `latest`, y hace `docker push`).
3. Tras la primera publicación, en la página del paquete de GitHub ajuste su visibilidad (pública) y su vínculo con el repositorio.

## Seguridad de los flujos

- Permisos: `contents: read` en todo el flujo; `contents: write` solo en `publish`. Sin secretos propios.
- `ci.yml` usa `pull_request` (no `pull_request_target`): los PR de forks corren sin secretos y con el token de solo lectura; no sube artefactos.
- `release.yml` no usa cachés (`cache: false`): lo que se publica se construye desde cero. `ci.yml` sí usa la caché de Go y de pnpm.
- Acciones: solo `actions/*` y `pnpm/action-setup` por etiqueta mayor (`@v4`, `@v5`). Fijarlas por SHA de commit es más estricto
  pero exige resolverlo contra GitHub (no se hizo sin red); conviene hacerlo con Dependabot (`github-actions`) antes de abrir el repositorio.
- Runners `ubuntu-24.04-arm` y `windows-11-arm`: gratuitos solo en repositorios PÚBLICOS. En uno privado los jobs arm64 se quedan
  en cola: quítelos de la matriz o use un runner propio.
- Los pasos de PowerShell fijan `$PSNativeCommandUseErrorActionPreference = $true` para que un comando nativo que falla detenga el paso.
