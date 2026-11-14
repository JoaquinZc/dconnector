# dconnector-server: imagen mínima y sin privilegios. Se construye desde la RAÍZ del repositorio:
#   docker build -f deploy/Dockerfile --build-arg VERSION=0.3.3 -t dconnector-server .
ARG GO_VERSION=1.27.4

# Se compila en la plataforma del constructor y se cruza al destino (sin emulador): CGO está desactivado.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -buildvcs=false \
      -ldflags "-s -w -X dconnector/internal/buildinfo.Version=${VERSION}" \
      -o /out/dconnector-server ./cmd/dconnector-server \
 && mkdir -p /out/data

# distroless/static: sin shell ni gestor de paquetes; trae certificados raíz y zonas horarias; usuario 65532 (nonroot).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dconnector-server /dconnector-server
COPY --from=build --chown=65532:65532 /out/data /data
# Licencia (AGPL-3.0) y avisos de terceros junto al binario.
COPY --from=build /src/LICENSE /src/LICENCIAS.txt /src/THIRD-PARTY-NOTICES /licenses/
ARG VERSION=dev
LABEL org.opencontainers.image.title="dconnector-server" org.opencontainers.image.version="${VERSION}"
ENV DCONNECTOR_DATA=/data \
    DCONNECTOR_LISTEN=0.0.0.0:8443
VOLUME ["/data"]
EXPOSE 8443
USER 65532:65532
# Usa el propio binario: consulta GET /api/v1/health (HTTPS con el certificado propio o HTTP si no hay TLS).
HEALTHCHECK --interval=30s --timeout=6s --start-period=20s --retries=3 \
  CMD ["/dconnector-server", "healthcheck"]
ENTRYPOINT ["/dconnector-server"]
