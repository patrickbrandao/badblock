# syntax=docker/dockerfile:1
#
# collector-roothints: binário Go estático numa imagem distroless (sem shell),
# rodando como usuário não-root. Não tem porta nem API HTTP.

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
# Build só com os arquivos da pasta: dependências de vendor/, sem rede.
ENV GOFLAGS=-mod=vendor GOPROXY=off GOTOOLCHAIN=local
WORKDIR /src
COPY . .
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w \
        -X github.com/patrickbrandao/badblock/apps/roothints/collector/internal/buildinfo.Version=${VERSION} \
        -X github.com/patrickbrandao/badblock/apps/roothints/collector/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/patrickbrandao/badblock/apps/roothints/collector/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/collector-roothints ./cmd/collector-roothints

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="badblock-collector-roothints" \
      org.opencontainers.image.description="Importa os servidores raiz do DNS (named.root da InterNIC, root hints) para o PostgreSQL do BadBlock" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/collector-roothints /collector-roothints
USER nonroot:nonroot
ENTRYPOINT ["/collector-roothints"]
