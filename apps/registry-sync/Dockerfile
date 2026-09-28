# syntax=docker/dockerfile:1
#
# registry-sync: binário Go estático numa imagem distroless (sem shell), rodando
# como usuário não-root. O healthcheck usa o próprio binário.

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w \
        -X github.com/patrickbrandao/badblock/apps/registry-sync/internal/buildinfo.Version=${VERSION} \
        -X github.com/patrickbrandao/badblock/apps/registry-sync/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/patrickbrandao/badblock/apps/registry-sync/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/registry-sync ./cmd/registry-sync \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="badblock-registry-sync" \
      org.opencontainers.image.description="Importa e sincroniza ASNs e prefixos IP (IANA, RIRs, NIC.br) com o PostgreSQL do BadBlock" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/registry-sync /registry-sync
# O volume de dados precisa pertencer ao usuário nonroot (65532) já na imagem:
# o Docker copia essa permissão para o volume nomeado na primeira montagem.
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot:nonroot
EXPOSE 8002
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/registry-sync", "healthcheck"]
ENTRYPOINT ["/registry-sync"]
