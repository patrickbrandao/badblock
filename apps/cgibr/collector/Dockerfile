# syntax=docker/dockerfile:1
#
# collector-cgibr: binário Go estático numa imagem distroless (sem shell),
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
        -X github.com/patrickbrandao/badblock/apps/cgibr/collector/internal/buildinfo.Version=${VERSION} \
        -X github.com/patrickbrandao/badblock/apps/cgibr/collector/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/patrickbrandao/badblock/apps/cgibr/collector/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/collector-cgibr ./cmd/collector-cgibr

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="badblock-collector-cgibr" \
      org.opencontainers.image.description="Importa os ASNs e blocos IP brasileiros do NIC.br (registro.br) para o PostgreSQL do BadBlock" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/collector-cgibr /collector-cgibr
USER nonroot:nonroot
ENTRYPOINT ["/collector-cgibr"]
