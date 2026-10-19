# Test file server: standalone Samba with registry shares + ad-manager agent
# (binary mounted from dist/). Local groups stand in for the domain groups.
FROM ubuntu:26.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq \
 && apt-get install -y -qq --no-install-recommends samba smbclient acl python3-samba openssl ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd financeiro && groupadd diretoria && useradd -m -G financeiro ana \
 && mkdir -p /srv/shares /opt/ad-manager
COPY smb.conf /etc/samba/smb.conf
COPY entrypoint.sh /entrypoint.sh
EXPOSE 9443 445
ENTRYPOINT ["/entrypoint.sh"]
