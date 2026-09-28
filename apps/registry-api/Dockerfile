# syntax=docker/dockerfile:1
#
# registry-api: binário Go estático numa imagem distroless (sem shell), rodando
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
        -X github.com/patrickbrandao/badblock/apps/registry-api/internal/buildinfo.Version=${VERSION} \
        -X github.com/patrickbrandao/badblock/apps/registry-api/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/patrickbrandao/badblock/apps/registry-api/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/registry-api ./cmd/registry-api

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="badblock-registry-api" \
      org.opencontainers.image.description="API HTTP de consulta de ASNs e prefixos IP do BadBlock" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/registry-api /registry-api
USER nonroot:nonroot
EXPOSE 8001
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/registry-api", "healthcheck"]
ENTRYPOINT ["/registry-api"]
