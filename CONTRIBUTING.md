# Cómo colaborar en dConnector

Gracias por querer mejorar dConnector. Este documento resume cómo compilar, probar y proponer cambios.

## Antes de empezar

- Lee el [`README.md`](README.md) (qué es y cómo se instala) y [`docs/02-ARQUITECTURA.md`](docs/02-ARQUITECTURA.md) (cómo está organizado).
- Para vulnerabilidades **no abras un issue**: sigue [`SECURITY.md`](SECURITY.md).
- Para cambios grandes (un canal nuevo, cambios de la API o del protocolo), abre primero un issue y cuéntanos la idea.
- Si el código y un documento de diseño discrepan, **manda el código**.

## Compilar y probar

Herramientas y versiones exactas en la sección «Compilar desde el código» del README (Go 1.27.4, Wails v3.1.0, Task 3.54.0, Node 22.21.1, pnpm 12.11.2, TypeScript 7.0.2; WiX 5.0.2 y .NET 8 solo para el MSI). Todo se compila **sin CGO**.

```powershell
pnpm -C frontend install --frozen-lockfile    # en un git worktree: añade --ignore-workspace
task gate                                      # pruebas + compilación + --version + --selftest
```

Lo mínimo antes de proponer un cambio (la «puerta»):

```powershell
gofmt -l .                                     # no debe listar nada
go vet ./...
go test ./...                                  # con -race si tienes un compilador de C
staticcheck ./...                              # staticcheck.conf desactiva solo ST1005
pnpm -C frontend exec tsc --noEmit
pnpm -C frontend exec vitest run
```

Si tocas el servidor, comprueba también que compila para las plataformas de destino:

```sh
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build ./cmd/dconnector-server
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build ./cmd/dconnector-server
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build ./cmd/dconnector-server
```

## Reglas del proyecto

1. **El núcleo no importa adaptadores.** `internal/core` no puede importar nada de `internal/adapter` ni librerías de canal (WhatsApp, Telegram, Meta). Los adaptadores implementan los puertos del núcleo. Hay una prueba de arquitectura (`arch_test.go`) que lo vigila: debe seguir en verde. Del mismo modo, el servidor no importa Wails.
2. **Cambios aditivos en el contrato.** Las firmas de `core/model`, `core/capability` y `core/port` solo crecen. Lo mismo vale para `/api/v1` y el protocolo `v1`: se añade, no se rompe.
3. **Código en inglés; textos visibles en español con tildes.** Los identificadores, nombres de archivos y de paquetes van en inglés. Los comentarios, los mensajes de error que ve el usuario, los textos de la interfaz y la documentación van en **español**, con tildes y con tono llano.
4. **Pruebas obligatorias.** Cada paquete nuevo y cada corrección de un fallo llevan sus pruebas. La API necesita pruebas de contrato y de seguridad. Nada de `TODO` sin dueño.
5. **Nunca pruebes con cuentas reales.** Ni en las pruebas automáticas ni en las manuales del repositorio: usa el adaptador falso (`internal/core/fake`) y pruebas de integración locales. Si necesitas datos reales para investigar, trabaja siempre con una **copia** y no la subas a ningún sitio. Las pruebas en vivo las hace cada persona con sus propias cuentas (`docs/QA-EN-VIVO.md`).
6. **Sin secretos ni datos personales en el repositorio.** Nada de claves, tokens, cookies, bases de datos, números de teléfono, correos, nombres reales, identificadores de cuentas reales ni rutas con tu nombre de usuario. Ni en el código, ni en las pruebas, ni en los documentos, ni en las capturas. El servidor **nunca** registra claves, tokens ni consultas.
7. **Errores honestos y accionables.** Todo error visible dice qué pasó y qué hacer, en español; un error interno no filtra rutas ni trazas al usuario. La aplicación nunca se queda en blanco si el servidor cae.
8. **Dependencias.** Una dependencia nueva debe justificarse (licencia compatible con la AGPL-3.0, mantenimiento, tamaño) y obliga a regenerar los avisos con `task licencias` (`LICENCIAS.txt` y `THIRD-PARTY-NOTICES`).
9. **Sin firma de código ni telemetría.** No se añaden llamadas de red que no sean las de los canales y los destinos configurados.

## Estilo de commits

- Un commit = un cambio con sentido, con el mensaje **en español**: una primera línea corta en imperativo o descriptiva (por ejemplo «Servidor: arranque y apagado, servicio de Windows, systemd, Docker y pruebas») y, si hace falta, un párrafo con el porqué.
- Prefija con el área cuando ayude: `Servidor:`, `Escritorio:`, `Frontend:`, `Seguridad:`, `docs:`.
- No mezcles reformateos masivos con cambios de comportamiento.

## Cómo proponer un cambio

1. Haz un *fork* y crea una rama desde la principal.
2. Haz el cambio con sus pruebas y pasa la puerta de arriba.
3. Abre un *pull request* que explique **qué** cambia, **por qué** y **cómo lo probaste** (y qué no pudiste probar: Linux, arm64, canales reales).
4. Responde a la revisión; los cambios que toquen seguridad, la API o el protocolo se revisan con más cuidado.

### Certificado de origen (DCO), opcional

Si quieres, firma tus commits con `git commit -s`, que añade `Signed-off-by: Tu Nombre <tu-correo>` y significa que tienes derecho a aportar ese código bajo la licencia del proyecto (el [Developer Certificate of Origin](https://developercertificate.org/)). No es obligatorio, pero se agradece. Usa un correo que quieras hacer público o el correo `noreply` de GitHub.

## Licencia de las aportaciones

dConnector se publica bajo **AGPL-3.0** (ver [`LICENSE`](LICENSE)). Al aportar código aceptas que se publique bajo esa misma licencia.
