# syntax=docker/dockerfile:1
#
# api-registrobr-rdapnum: binário Go estático numa imagem distroless (sem shell), rodando
# como usuário não-root. O healthcheck usa o próprio binário.

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
        -X github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/buildinfo.Version=${VERSION} \
        -X github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/api-registrobr-rdapnum ./cmd/api-registrobr-rdapnum

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="badblock-api-registrobr-rdapnum" \
      org.opencontainers.image.description="API HTTP do RDAP do registro.br por ASN e bloco, coletado pelo BadBlock" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/api-registrobr-rdapnum /api-registrobr-rdapnum
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/api-registrobr-rdapnum", "healthcheck"]
ENTRYPOINT ["/api-registrobr-rdapnum"]
