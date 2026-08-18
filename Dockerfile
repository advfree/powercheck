# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:24-alpine AS web-build
WORKDIR /src/web-prototype
COPY web-prototype/package.json web-prototype/package-lock.json web-prototype/.npmrc ./
RUN npm ci
COPY web-prototype/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS manager-build
WORKDIR /src
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
COPY go.mod ./
COPY cmd/powercheck-manager/ ./cmd/powercheck-manager/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -trimpath \
    -ldflags="-s -w -X powercheck/internal/buildinfo.Version=$VERSION -X powercheck/internal/buildinfo.Commit=$COMMIT -X powercheck/internal/buildinfo.Date=$BUILD_DATE" \
    -o /out/powercheck-manager ./cmd/powercheck-manager

FROM alpine:3.23 AS runtime
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="PowerCheck Manager" \
      org.opencontainers.image.description="Web manager for PowerCheck PVE and NUT monitoring" \
      org.opencontainers.image.source="https://github.com/advfree/powercheck" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.created="$BUILD_DATE"

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 powercheck \
    && adduser -S -D -H -u 10001 -G powercheck powercheck \
    && mkdir -p /app/web /data /config \
    && chown -R powercheck:powercheck /app /data /config

COPY --from=manager-build --chown=powercheck:powercheck /out/powercheck-manager /usr/local/bin/powercheck-manager
COPY --from=web-build --chown=powercheck:powercheck /src/web-prototype/dist/client/ /app/web/

USER 10001:10001
WORKDIR /app
EXPOSE 8765
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD sh -c 'wget -q -T 2 -O /dev/null "${POWERCHECK_HEALTH_URL:-http://127.0.0.1:8765/}"'

ENTRYPOINT ["/usr/local/bin/powercheck-manager"]
CMD ["-listen=0.0.0.0:8765", "-web-root=/app/web", "-event-file=/data/events.jsonl", "-nut-history-file=/data/ups-history.jsonl"]
