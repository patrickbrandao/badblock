# syntax=docker/dockerfile:1
#
# api-arin: binário Go estático numa imagem distroless (sem shell), rodando
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
        -X github.com/patrickbrandao/badblock/apps/arin/api/internal/buildinfo.Version=${VERSION} \
        -X github.com/patrickbrandao/badblock/apps/arin/api/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/patrickbrandao/badblock/apps/arin/api/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/api-arin ./cmd/api-arin

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="badblock-api-arin" \
      org.opencontainers.image.description="API HTTP das delegações de ASNs e blocos IP do RIR ARIN (delegated-extended), do BadBlock" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/api-arin /api-arin
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/api-arin", "healthcheck"]
ENTRYPOINT ["/api-arin"]
