# ── frontend (SPA Vue + Vite+ → web/dist) ─────────────────────────────────────
FROM node:24-bookworm-slim AS frontend
WORKDIR /fe
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ── build ─────────────────────────────────────────────────────────────────────
FROM golang:1.27-alpine AS build
WORKDIR /src
ARG VERSION=dev
ARG COMMIT=none
ARG BUILDTIME=unknown
COPY go.mod go.sum ./
# the replaced modules (go.mod: third_party/, docs/vendor.md) before the download
COPY third_party/ ./third_party/
RUN go mod download
COPY . .
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w \
      -X ad-manager/internal/buildinfo.Version=${VERSION} \
      -X ad-manager/internal/buildinfo.Commit=${COMMIT} \
      -X ad-manager/internal/buildinfo.BuildTime=${BUILDTIME}" \
    -o /out/ad-manager .

# ── runtime ───────────────────────────────────────────────────────────────────
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app && mkdir -p /opt/ad-manager/logs && chown app /opt/ad-manager/logs
COPY --from=build /out/ad-manager /usr/local/bin/ad-manager
COPY config.toml.example /opt/ad-manager/config.toml
USER app
EXPOSE 8443
# In production mount the certificate and adjust /opt/ad-manager/config.toml
# (server.listen = "0.0.0.0:8443", cert_file, key_file, ad.url, ad.ca_file).
ENTRYPOINT ["/usr/local/bin/ad-manager"]
CMD ["server"]
