# ad-manager all-in-one (docs/docker.md): a Samba 4 AD domain with the panel and the agent, to try the AD Manager with
# one `docker run`. For tests and demos; a production DC is installed with the .deb (docs/installation.md).
#
#   docker build -f docker/allinone/Dockerfile -t jniltinho/ad-manager .
#   docker run -d --name ad-manager --hostname dc1 --stop-timeout 60 --cap-add SYS_ADMIN -p 8443:8443 jniltinho/ad-manager

# ── frontend (SPA Vue + Vite+ → web/dist) ─────────────────────────────────────
FROM node:24-bookworm-slim AS frontend
WORKDIR /fe
COPY frontend/package*.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ── binary (panel, agent and CLI in one) ─────────────────────────────────────
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

# ── runtime: Ubuntu 26.04 with the distro's Samba 4, Redis and supervisord ───
FROM ubuntu:26.04
ARG VERSION=dev
LABEL org.opencontainers.image.title="ad-manager" \
      org.opencontainers.image.description="Samba 4 AD domain with the AD Manager panel and agent (demo)" \
      org.opencontainers.image.version="${VERSION}"
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq \
 && apt-get install -y -qq --no-install-recommends \
      samba-ad-dc samba-ad-provision winbind krb5-user smbclient ldb-tools python3-samba python3-setproctitle \
      acl attr bind9-dnsutils redis-server supervisor openssl ca-certificates curl tzdata \
 && rm -rf /var/lib/apt/lists/* /etc/samba/smb.conf \
 && adduser --system --group --home /opt/ad-manager/data --no-create-home --shell /usr/sbin/nologin ad-manager \
 && usermod -aG adm ad-manager \
 && install -d -m 0750 -o ad-manager -g ad-manager /opt/ad-manager/data /opt/ad-manager/logs \
 && install -d -m 0750 -o root -g ad-manager /opt/ad-manager/tls
COPY --from=build /out/ad-manager /opt/ad-manager/ad-manager
COPY docker/allinone/entrypoint.sh /entrypoint.sh
COPY docker/allinone/supervisord.conf /etc/supervisor/supervisord.conf
COPY scripts/samba-setup.py /opt/ad-manager/scripts/samba-setup.py
COPY README.md /opt/ad-manager/README.md
COPY docs/*.md /opt/ad-manager/docs/
RUN ln -s /opt/ad-manager/ad-manager /usr/bin/ad-manager && chmod 0755 /entrypoint.sh
# the domain, the panel's data, the certificates and the shares survive a new container
VOLUME ["/var/lib/samba", "/etc/samba", "/opt/ad-manager/data", "/opt/ad-manager/tls", "/srv/shares"]
# 8443 panel · 9443 agent · the DC: 53 DNS, 88/464 Kerberos, 135 RPC, 139/445 SMB, 389/636 LDAP(S), 3268/3269 GC
EXPOSE 8443 9443 53/tcp 53/udp 88/tcp 88/udp 135 139 389/tcp 389/udp 445 464/tcp 464/udp 636 3268 3269
HEALTHCHECK --interval=15s --timeout=5s --start-period=90s --retries=8 \
  CMD curl -fsk https://127.0.0.1:8443/api/health >/dev/null || exit 1
ENTRYPOINT ["/entrypoint.sh"]
