#!/usr/bin/env bash
# The Ubuntu client of destino.lab (lab-clients.sh), as root on the machine:
#   ubuntu-client.sh join     — realmd/sssd by the delegated join account (its password on stdin), OU=Estacoes,
#                               home folder created on the first logon (pam_mkhomedir), DNS through the domain's DC
#   ubuntu-client.sh check    — a domain user (its password on stdin): id, an SSH logon with password on this machine
#                               (PAM: home folder, Kerberos ticket from sssd), a share read with that ticket
#   ubuntu-client.sh adsys    — ADSys (Ubuntu's own policies from GPOs): what works without Ubuntu Pro
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
REALM=destino.lab DC=192.168.57.40 OU="OU=Estacoes,DC=destino,DC=lab"

case $1 in
join)
  read -r pw
  if realm list --name-only 2>/dev/null | grep -qx "$REALM"; then
    if adcli testjoin -D "$REALM" >/dev/null 2>&1; then echo "already in $REALM"; exit 0; fi
    # a lab domain created again no longer knows this machine: leave it locally, then join the new one
    realm leave "$REALM" >/dev/null 2>&1 || true
    # the old domain's home folders belong to uids the new one does not have: set aside, never deleted
    for h in /home/*@"$REALM"; do [ -d "$h" ] && mv "$h" "$h.stale-$(date +%Y%m%d%H%M%S)"; done
    stale=1
    echo "left the stale $REALM"
  fi
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq realmd sssd sssd-tools adcli krb5-user libnss-sss libpam-sss \
    smbclient sshpass openssh-server bind9-dnsutils >/dev/null # bind9-dnsutils: nsupdate, sssd's dynamic DNS
  systemctl disable --now systemd-resolved >/dev/null 2>&1 || true
  rm -f /etc/resolv.conf
  printf 'nameserver %s\nsearch %s\n' "$DC" "$REALM" >/etc/resolv.conf
  # the FQDN: the computer account's DNS name and the one sssd registers
  hostnamectl set-hostname "$(hostname -s).$REALM"
  printf '%s\n' "$pw" | realm join --user=lab.ingresso --computer-ou="$OU" "$REALM"
  pam-auth-update --enable mkhomedir
  # sssd registers this machine in the domain's DNS with the card that reaches the DC: left to itself it picks
  # Vagrant's NAT card (10.0.2.15) and registers nothing
  iface=$(ip -o route get "$DC" | sed -n 's/.* dev \([^ ]*\).*/\1/p')
  sed -i "/^\[domain\/$REALM\]/a dyndns_update = True\ndyndns_iface = $iface\ndyndns_update_ptr = False" /etc/sssd/sssd.conf
  systemctl restart sssd
  # the old domain's SIDs stay in sssd's cache and keep group names from resolving: a clean cache (local data kept)
  [ -z "${stale:-}" ] || sssctl cache-remove -p -o -s >/dev/null
  # sssd 2.10 (Ubuntu 26.04) may not send its dynamic update after the join: wait for it, else register the address
  # the way a Windows member does, a secure (GSS-TSIG) update with the machine's own keytab
  ip=$(ip -o -4 addr show dev "$iface" | awk '{print $4}' | cut -d/ -f1)
  for _ in $(seq 12); do host "$(hostname)" "$DC" >/dev/null 2>&1 && break; sleep 5; done
  if ! host "$(hostname)" "$DC" >/dev/null 2>&1; then
    KRB5CCNAME=/tmp/dyndns.cc kinit -k "$(hostname -s | tr a-z A-Z)\$@${REALM^^}"
    printf 'server %s\nzone %s\nupdate add %s 3600 A %s\nsend\n' "$DC" "$REALM" "$(hostname)" "$ip" |
      KRB5CCNAME=/tmp/dyndns.cc nsupdate -g
    rm -f /tmp/dyndns.cc
  fi
  echo "DNS: $(host "$(hostname)" "$DC" | tail -1)"
  # domain users log on by password through SSH (the lab's check); the Vagrant key stays for vagrant
  printf 'PasswordAuthentication yes\nKbdInteractiveAuthentication yes\n' >/etc/ssh/sshd_config.d/60-lab-domain.conf
  systemctl restart ssh
  echo "$(hostname) joined $REALM ($OU)"
  ;;
check)
  read -r pw
  u=lab.ana@$REALM
  echo "id: $(id "$u" | cut -c1-120)"
  # SSHPASS in the environment (sshpass -e), never an argument
  SSHPASS=$pw sshpass -e ssh -o StrictHostKeyChecking=no -o PubkeyAuthentication=no "$u@localhost" \
    'echo "logon: $(whoami) in $PWD"; t=$(klist 2>/dev/null | grep -m1 krbtgt | awk "{print \$NF}"); echo "ticket: $t"; s=$(smbclient //dstfs.destino.lab/Publico --use-kerberos=required -c "get readme.txt -" 2>/dev/null | head -1); echo "share (Kerberos): $s"; [ -n "$t" ] && [ -n "$s" ]'
  echo "home folder: $(ls -d "/home/$u" 2>/dev/null || echo absent)"
  ;;
adsys)
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq adsys >/dev/null 2>&1 || { echo "adsys: package unavailable"; exit 0; }
  echo "adsys: $(adsysctl version 2>/dev/null | head -1)"
  echo "computer policies: $(adsysctl policy applied --machine 2>&1 | tail -3 | tr '\n' ' ')"
  ;;
*)
  sed -n '2,8p' "$0"
  exit 1
  ;;
esac
