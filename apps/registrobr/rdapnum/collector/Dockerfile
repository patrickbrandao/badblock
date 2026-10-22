# syntax=docker/dockerfile:1
#
# collector-registrobr-rdapnum: binário Go estático numa imagem distroless
# (sem shell), rodando como usuário não-root. Não tem porta nem API HTTP.
# A imagem distroless traz os certificados das CAs (o RDAP é HTTPS).

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
        -X github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/buildinfo.Version=${VERSION} \
        -X github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/collector-registrobr-rdapnum ./cmd/collector-registrobr-rdapnum

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="badblock-collector-registrobr-rdapnum" \
      org.opencontainers.image.description="Coleta o RDAP do registro.br de cada ASN e bloco brasileiro para o PostgreSQL do BadBlock" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/collector-registrobr-rdapnum /collector-registrobr-rdapnum
USER nonroot:nonroot
ENTRYPOINT ["/collector-registrobr-rdapnum"]
