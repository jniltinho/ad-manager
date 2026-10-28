# Test Samba AD DC — Ubuntu 26.04 (or BASE=debian:13) + distro packages. Only for
# integration tests and for the panel in development; throwaway domain.
ARG BASE=ubuntu:26.04
FROM ${BASE}
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq \
 && apt-get install -y -qq --no-install-recommends samba-ad-dc samba-ad-provision winbind krb5-user smbclient ldb-tools python3-setproctitle \
 && rm -rf /var/lib/apt/lists/* /etc/samba/smb.conf
COPY entrypoint.sh /entrypoint.sh
EXPOSE 636
HEALTHCHECK --interval=3s --timeout=3s --retries=40 CMD bash -c '</dev/tcp/127.0.0.1/636'
ENTRYPOINT ["/entrypoint.sh"]
