# syntax=docker/dockerfile:1
# Hister runtime for the first Theophysics slice: local search and previews.
# Crawl-scale services and optional yt-dlp/media extraction are not included.
FROM node:24.22.1-bookworm-slim AS frontend
WORKDIR /src
COPY package.json package-lock.json ./
COPY webui/app/package.json webui/app/
COPY webui/components/package.json webui/components/
RUN --mount=type=secret,id=proxy_ca \
    --mount=type=cache,id=tp-npm,target=/root/.npm \
    if [ -s /run/secrets/proxy_ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/proxy_ca; fi; \
    npm ci --workspace=@hister/app --workspace=@hister/components
COPY webui/app/ webui/app/
COPY webui/components/ webui/components/
RUN npm run build --workspace=@hister/app

FROM golang:1.26.11-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=secret,id=proxy_ca \
    --mount=type=cache,target=/go/pkg/mod \
    if [ -s /run/secrets/proxy_ca ]; then export SSL_CERT_FILE=/run/secrets/proxy_ca; fi; \
    go mod download
COPY . .
COPY --from=frontend /src/webui/app/build/ server/static/app/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -p 4 -mod=readonly -trimpath -ldflags='-s -w' -o /out/hister .

FROM debian:bookworm-slim AS release
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /hister/data && chown 65532:65532 /hister/data
COPY --from=build /out/hister /usr/local/bin/hister
USER 65532:65532
ENV XDG_STATE_HOME=/hister/data XDG_CONFIG_HOME=/hister/data/config \
    HISTER__SERVER__ADDRESS=0.0.0.0:4433 HISTER__SERVER__BASE_URL=http://hister:4433
WORKDIR /hister/data
EXPOSE 4433
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=5 \
    CMD ["curl", "-fsS", "--max-time", "4", "http://127.0.0.1:4433/health"]
ENTRYPOINT ["/usr/local/bin/hister"]
CMD ["listen"]
