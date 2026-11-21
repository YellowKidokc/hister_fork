# syntax=docker/dockerfile:1
FROM golang:1.26.11-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
# Managed cloud builds may pass the supplied CA as a BuildKit secret. Normal
# builds use the image's standard trust store; verification remains enabled.
RUN --mount=type=secret,id=proxy_ca \
    --mount=type=cache,target=/go/pkg/mod \
    if [ -s /run/secrets/proxy_ca ]; then export SSL_CERT_FILE=/run/secrets/proxy_ca; fi; \
    go mod download
COPY config/ config/
COPY files/ files/
COPY client/ client/
COPY server/types/ server/types/
COPY server/languages/ server/languages/
COPY server/errors/ server/errors/
COPY server/oauth/ server/oauth/
COPY server/sanitizer/ server/sanitizer/
COPY internal/research/ internal/research/
COPY cmd/theophysics/ cmd/theophysics/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags='-s -w' -o /out/theophysics ./cmd/theophysics

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir /data && chown 65532:65532 /data
COPY --from=build /out/theophysics /usr/local/bin/theophysics
USER 65532:65532
ENV TP_ADDRESS=0.0.0.0:4444 TP_DATA_DIR=/data
EXPOSE 4444
HEALTHCHECK --interval=15s --timeout=5s --start-period=15s --retries=5 \
    CMD ["curl", "-fsS", "--max-time", "4", "http://127.0.0.1:4444/healthz"]
ENTRYPOINT ["/usr/local/bin/theophysics"]
