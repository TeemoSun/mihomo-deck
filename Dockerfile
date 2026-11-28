# syntax=docker/dockerfile:1

# Preserve BuildKit's automatic target values, with Linux/AMD64 fallbacks.
ARG TARGETOS=${TARGETOS:-linux}
ARG TARGETARCH=${TARGETARCH:-amd64}

FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
ARG NPM_CONFIG_REGISTRY=https://registry.npmjs.org
ENV NPM_CONFIG_REGISTRY=${NPM_CONFIG_REGISTRY}
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS backend-builder
ARG TARGETOS
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY} \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN go build -buildvcs=false -trimpath -ldflags='-s -w' -o /build/mihomo-deck ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata su-exec \
    && addgroup -g 1000 -S appuser \
    && adduser -u 1000 -S appuser -G appuser \
    && mkdir -p /data /app/static \
    && chown -R appuser:appuser /data /app
WORKDIR /app
COPY --from=backend-builder /build/mihomo-deck /app/mihomo-deck
COPY --from=frontend-builder /app/frontend/dist /app/static
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK CMD ["su-exec", "appuser", "/app/mihomo-deck", "-healthcheck"]
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/app/mihomo-deck"]
